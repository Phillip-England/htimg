package renderer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

var ErrInvalidRequest = errors.New("invalid render request")

type Request struct {
	HTML              string `json:"html"`
	CSS               string `json:"css"`
	URL               string `json:"url"`
	Width             int64  `json:"width"`
	Height            int64  `json:"height"`
	DeviceScaleFactor int64  `json:"deviceScaleFactor"`
	Filename          string `json:"filename"`
}

type Renderer struct {
	browserPath string
}

func New() *Renderer {
	return &Renderer{browserPath: findBrowserPath()}
}

func (r *Renderer) Render(ctx context.Context, req Request) ([]byte, error) {
	req = normalize(req)
	if err := validate(req); err != nil {
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.WindowSize(int(req.Width), int(req.Height)),
	)
	if r.browserPath != "" {
		opts = append(opts, chromedp.ExecPath(r.browserPath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(timeoutCtx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var image []byte
	actions := []chromedp.Action{
		emulation.SetDeviceMetricsOverride(req.Width, req.Height, float64(req.DeviceScaleFactor), false),
	}
	if req.URL != "" {
		actions = append(actions, chromedp.Navigate(req.URL))
	} else {
		doc := document(req.HTML, req.CSS)
		actions = append(actions,
			chromedp.Navigate("about:blank"),
			chromedp.ActionFunc(func(ctx context.Context) error {
				tree, err := page.GetFrameTree().Do(ctx)
				if err != nil {
					return err
				}
				return page.SetDocumentContent(tree.Frame.ID, doc).Do(ctx)
			}),
		)
	}
	actions = append(actions,
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.ActionFunc(func(ctx context.Context) error {
			clip, err := fullPageClip(ctx, req)
			if err != nil {
				return err
			}

			image, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithCaptureBeyondViewport(true).
				WithClip(clip).
				Do(ctx)
			return err
		}),
	)
	err := chromedp.Run(browserCtx, actions...)
	if err != nil {
		if r.browserPath == "" {
			return nil, fmt.Errorf("render failed: %w; install Chrome or Chromium, or set CHROME_PATH", err)
		}
		return nil, fmt.Errorf("render failed: %w", err)
	}

	return image, nil
}

func fullPageClip(ctx context.Context, req Request) (*page.Viewport, error) {
	_, _, _, _, _, cssContentSize, err := page.GetLayoutMetrics().Do(ctx)
	if err != nil {
		return nil, err
	}

	width := float64(req.Width)
	height := float64(req.Height)
	if cssContentSize != nil {
		width = math.Max(width, math.Ceil(cssContentSize.Width))
		height = math.Max(height, math.Ceil(cssContentSize.Height))
	}

	return &page.Viewport{
		X:      0,
		Y:      0,
		Width:  width,
		Height: height,
		Scale:  1,
	}, nil
}

func normalize(req Request) Request {
	req.URL = strings.TrimSpace(req.URL)
	if req.Width == 0 {
		req.Width = 1200
	}
	if req.Height == 0 {
		req.Height = 800
	}
	if req.DeviceScaleFactor == 0 {
		req.DeviceScaleFactor = 1
	}
	return req
}

func validate(req Request) error {
	hasHTML := strings.TrimSpace(req.HTML) != ""
	hasURL := strings.TrimSpace(req.URL) != ""
	if hasHTML == hasURL {
		return fmt.Errorf("%w: provide exactly one of html or url", ErrInvalidRequest)
	}
	if hasURL {
		parsed, err := url.ParseRequestURI(req.URL)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("%w: url must be absolute", ErrInvalidRequest)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%w: url must use http or https", ErrInvalidRequest)
		}
	}
	if req.Width < 100 || req.Width > 5000 {
		return fmt.Errorf("%w: width must be between 100 and 5000", ErrInvalidRequest)
	}
	if req.Height < 100 || req.Height > 5000 {
		return fmt.Errorf("%w: height must be between 100 and 5000", ErrInvalidRequest)
	}
	if req.DeviceScaleFactor < 1 || req.DeviceScaleFactor > 4 {
		return fmt.Errorf("%w: deviceScaleFactor must be between 1 and 4", ErrInvalidRequest)
	}
	return nil
}

func document(markup, css string) string {
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
html, body { margin: 0; min-height: 100%; }
*, *::before, *::after { box-sizing: border-box; }
` + css + `
</style>
</head>
<body>` + markup + `</body>
</html>`
}

func findBrowserPath() string {
	if path := os.Getenv("CHROME_PATH"); path != "" {
		return path
	}

	candidates := []string{}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
