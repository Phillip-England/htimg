package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitCreatesEnvAndDatabase(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "config", ".env")

	if err := Init(envPath); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("env file stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "main.sqlite")); err != nil {
		t.Fatalf("database stat error = %v", err)
	}
}

func TestLoadConfigRequiresValues(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("ADMIN_USERNAME=admin\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadConfig(envPath)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
}

func TestLoadConfigResolvesDBPathRelativeToEnv(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "config", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte("ADMIN_USERNAME=admin\nADMIN_PASSWORD=secret\nSESSION_SECRET=secret\nDB_PATH=../data/main.sqlite\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfig(envPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := filepath.Join(root, "data", "main.sqlite")
	if cfg.DBPath != want {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, want)
	}
}

func TestLoginBlocksAfterFiveFailures(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "main.sqlite"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	manager := NewManager(Config{
		AdminUsername: "admin",
		AdminPassword: "secret",
		SessionSecret: "session-secret",
		DBPath:        "main.sqlite",
	}, db)
	manager.now = func() time.Time { return time.Unix(1000, 0) }

	for i := 0; i < 4; i++ {
		req := loginRequest("192.0.2.1:12345", "admin", "bad")
		rr := httptest.NewRecorder()
		manager.Login(rr, req)
		if rr.Code == http.StatusForbidden {
			t.Fatalf("attempt %d returned forbidden too early", i+1)
		}
	}

	req := loginRequest("192.0.2.1:12345", "admin", "bad")
	rr := httptest.NewRecorder()
	manager.Login(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("fifth failure status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestLoginSuccessRedirectsToProtectedApp(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "main.sqlite"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	manager := NewManager(Config{
		AdminUsername: "admin",
		AdminPassword: "secret",
		SessionSecret: "session-secret",
		DBPath:        "main.sqlite",
	}, db)

	req := loginRequest("192.0.2.1:12345", "admin", "secret")
	rr := httptest.NewRecorder()
	manager.Login(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	if location := rr.Header().Get("Location"); location != protectedAppPath {
		t.Fatalf("Location = %q, want %q", location, protectedAppPath)
	}
}

func loginRequest(remoteAddr, username, password string) *http.Request {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestMiddlewareRedirectsUnauthenticated(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "main.sqlite"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	manager := NewManager(Config{AdminUsername: "admin", AdminPassword: "secret", SessionSecret: "session-secret", DBPath: "main.sqlite"}, db)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	manager.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler should not run")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	if location := rr.Header().Get("Location"); location != "/login" {
		t.Fatalf("Location = %q, want /login", location)
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-host-port"
	if got := clientIP(req); got != req.RemoteAddr {
		t.Fatalf("clientIP() = %q, want %q", got, req.RemoteAddr)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), ".env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
}
