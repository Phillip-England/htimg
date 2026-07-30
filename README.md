# htimg

Local web app and API for turning HTML, CSS, or public URLs into PNG or JPEG screenshots.

## Requirements

- Go 1.26+
- Google Chrome or Chromium

On macOS, `htimg` checks the standard Chrome and Chromium app paths. On other systems, or for a custom browser install, set `CHROME_PATH`.

## Port

`htimg` serves on the dedicated local app port:

```text
http://127.0.0.1:9993
```

You can override the bind address with `--addr` or `HTIMG_ADDR`, but the documented default for this project is `127.0.0.1:9993`.

## Initialize

Initialize the app before the first run:

```sh
htimg init
```

During development, the same command works through Go:

```sh
go run . init
```

This creates the environment file if it does not already exist:

```text
./config/.env
```

It also initializes the SQLite database:

```text
./data/main.sqlite
```

The generated environment file contains a mock admin account and a random session secret:

```env
ADMIN_USERNAME=admin
ADMIN_PASSWORD=change-me-now
SESSION_SECRET=<random-secret>
DB_PATH=../data/main.sqlite
```

Change `ADMIN_PASSWORD` before using the app anywhere outside local development.

## Run

After initialization:

```sh
htimg
```

Or during development:

```sh
go run .
```

Open `http://127.0.0.1:9993` to view the public showcase page.

Optional flags:

```sh
htimg --env ./config/.env --addr 127.0.0.1:9993
```

Startup fails if the environment file is missing or any required value is empty.

## Docker

Run the app locally in Docker:

```sh
make docker
```

The target builds the image, publishes `http://127.0.0.1:9993`, and bind-mounts
`./config` and `./data` so the environment file and SQLite database persist.

## Operator Security

The app has one operator account configured in `./config/.env`.

Failed access attempts are tracked by client IP in SQLite. On every attempt, rows older than 24 hours are purged. An IP is blocked with HTTP 403 after 5 failures in the last 24 hours.

Sessions use signed, HTTP-only cookies and are also tracked server-side with a 12-hour lifetime. Use the in-app logout button to clear the browser cookie and server-side session.

For production deployments, use HTTPS and do not trust forwarded IP headers unless the app is explicitly placed behind a known trusted reverse proxy.

## API

`POST /api/render` requires an authenticated operator session. It accepts JSON and returns `image/png` or `image/jpeg`. Provide exactly one source: `html` for pasted markup, or `url` for a public `http` or `https` page.

```sh
curl -o snapshot.png \
  -H 'Content-Type: application/json' \
  -d '{
    "html": "<main><h1>Hello</h1></main>",
    "css": "body{display:grid;place-items:center;min-height:100vh}h1{font:64px sans-serif}",
    "width": 1200,
    "height": 800,
    "deviceScaleFactor": 1,
    "format": "png",
    "background": "solid"
  }' \
  http://127.0.0.1:9993/api/render
```

Render a public website URL:

```sh
curl -o snapshot.jpg \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com",
    "width": 1200,
    "height": 800,
    "deviceScaleFactor": 1,
    "format": "jpeg",
    "background": "solid"
  }' \
  http://127.0.0.1:9993/api/render
```

Request fields:

- `html`: markup rendered inside `<body>`; required when `url` is omitted
- `css`: optional CSS inserted into a `<style>` tag for pasted HTML
- `url`: public `http` or `https` URL to capture; required when `html` is omitted
- `width`: viewport width, default `1200`
- `height`: viewport height, default `800`
- `deviceScaleFactor`: output scale from `1` to `4`, default `1`
- `format`: `png` or `jpeg`, default `png`
- `background`: `solid` or `transparent`, default `solid`; transparency requires PNG
- `filename`: optional filename used in the response disposition
