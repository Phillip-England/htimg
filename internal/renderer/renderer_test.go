package renderer

import (
	"bytes"
	"errors"
	"image/png"
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	req := normalize(Request{HTML: "<main>Hello</main>"})

	if req.Width != 1200 {
		t.Fatalf("width = %d, want 1200", req.Width)
	}
	if req.Height != 800 {
		t.Fatalf("height = %d, want 800", req.Height)
	}
	if req.DeviceScaleFactor != 1 {
		t.Fatalf("deviceScaleFactor = %d, want 1", req.DeviceScaleFactor)
	}
}

func TestValidateRequiresSource(t *testing.T) {
	err := validate(normalize(Request{}))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateAcceptsURL(t *testing.T) {
	err := validate(normalize(Request{URL: " https://example.com/page "}))
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestValidateRejectsHTMLAndURL(t *testing.T) {
	err := validate(normalize(Request{HTML: "<p>ok</p>", URL: "https://example.com"}))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateRejectsUnsupportedURLScheme(t *testing.T) {
	err := validate(normalize(Request{URL: "file:///etc/passwd"}))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateBounds(t *testing.T) {
	req := normalize(Request{HTML: "<p>ok</p>", Width: 99})
	err := validate(req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestRenderCapturesFullPageHeight(t *testing.T) {
	if findBrowserPath() == "" {
		t.Skip("Chrome or Chromium not found")
	}

	image, err := New().Render(t.Context(), Request{
		HTML:   `<main class="page"><section>Top</section><section>Bottom</section></main>`,
		CSS:    `.page{height:2200px;background:#fff}.page section:last-child{margin-top:2000px}`,
		Width:  800,
		Height: 600,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	config, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if config.Width != 800 {
		t.Fatalf("image width = %d, want 800", config.Width)
	}
	if config.Height < 2200 {
		t.Fatalf("image height = %d, want at least 2200", config.Height)
	}
}
