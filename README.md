# htimg

Local web app and API for turning HTML, CSS, or public URLs into PNG screenshots.

## Requirements

- Go 1.26+
- Google Chrome or Chromium

On macOS, `htimg` checks the standard Chrome and Chromium app paths. On other systems, or for a custom browser install, set `CHROME_PATH`.

## Run

```sh
go run .
```

Open `http://localhost:8080`.

To bind a different address:

```sh
HTIMG_ADDR=:18080 go run .
```

## API

`POST /api/render` accepts JSON and returns `image/png`. Provide exactly one source:
`html` for pasted markup, or `url` for a public `http` or `https` page.

```sh
curl -o snapshot.png \
  -H 'Content-Type: application/json' \
  -d '{
    "html": "<main><h1>Hello</h1></main>",
    "css": "body{display:grid;place-items:center;min-height:100vh}h1{font:64px sans-serif}",
    "width": 1200,
    "height": 800,
    "deviceScaleFactor": 1
  }' \
  http://localhost:8080/api/render
```

Render a public website URL:

```sh
curl -o snapshot.png \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com",
    "width": 1200,
    "height": 800,
    "deviceScaleFactor": 1
  }' \
  http://localhost:8080/api/render
```

Request fields:

- `html`: markup rendered inside `<body>`; required when `url` is omitted
- `css`: optional CSS inserted into a `<style>` tag for pasted HTML
- `url`: public `http` or `https` URL to capture; required when `html` is omitted
- `width`: viewport width, default `1200`
- `height`: viewport height, default `800`
- `deviceScaleFactor`: output scale from `1` to `4`, default `1`
- `filename`: optional filename used in the response disposition
