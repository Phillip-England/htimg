FROM golang:1.26-bookworm AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/htimg .

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
		ca-certificates \
		chromium \
		fonts-liberation \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=build /out/htimg ./htimg

ENV CHROME_PATH=/usr/bin/chromium
ENV HTIMG_ENV=/app/config/.env

EXPOSE 9993
CMD ["./htimg", "--addr", "0.0.0.0:9993"]
