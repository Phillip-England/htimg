package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/phillip-england/htimg/internal/auth"
	"github.com/phillip-england/htimg/internal/renderer"
)

//go:embed web/* logo.webp
var webFS embed.FS

const maxRenderBodyBytes = 20 << 20
const defaultAddr = "127.0.0.1:8765"
const defaultEnvPath = "config/.env"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("htimg failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "init" {
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		envPath := fs.String("env", defaultEnvPath, "environment file path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := auth.Init(*envPath); err != nil {
			return err
		}
		slog.Info("htimg initialized", "env", *envPath)
		return nil
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", getenv("HTIMG_ADDR", defaultAddr), "listen address")
	envPath := fs.String("env", getenv("HTIMG_ENV", defaultEnvPath), "environment file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return serve(*addr, *envPath)
}

func serve(addr, envPath string) error {
	cfg, err := auth.LoadConfig(envPath)
	if err != nil {
		return fmt.Errorf("%w; run `htimg init` first", err)
	}
	db, err := auth.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	r := renderer.New()
	authManager := auth.NewManager(cfg, db)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", landingHandler)
	mux.Handle("GET /app", authManager.Middleware(http.HandlerFunc(indexHandler)))
	mux.HandleFunc("GET /logo.webp", logoHandler)
	mux.HandleFunc("GET /login", authManager.Login)
	mux.HandleFunc("POST /login", authManager.Login)
	mux.HandleFunc("POST /logout", authManager.Logout)
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.Handle("POST /api/render", authManager.Middleware(renderHandler(r)))
	mux.Handle("GET /web/", authManager.Middleware(http.FileServerFS(webFS)))

	server := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("htimg listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, webFS, "web/index.html")
}

func landingHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, webFS, "web/landing.html")
}

func logoHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, webFS, "logo.webp")
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func renderHandler(render *renderer.Renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req renderer.Request
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRenderBodyBytes)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		image, err := render.Render(r.Context(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, renderer.ErrInvalidRequest) {
				status = http.StatusBadRequest
			}
			writeJSONError(w, status, err.Error())
			return
		}

		format := responseFormat(req.Format)
		filename := req.Filename
		if filename == "" {
			filename = "snapshot." + format
		}
		w.Header().Set("Content-Type", contentType(format))
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		_, _ = w.Write(image)
	}
}

func contentType(format string) string {
	if format == "jpeg" {
		return "image/jpeg"
	}
	return "image/png"
}

func responseFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "jpeg" {
		return "jpeg"
	}
	return "png"
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
