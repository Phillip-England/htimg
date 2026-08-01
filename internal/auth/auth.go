package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sessionCookieName = "htimg_session"
	sessionLifetime   = 12 * time.Hour
	failureWindow     = 24 * time.Hour
	maxFailures       = 5
	protectedAppPath  = "/app"
	defaultDBPath     = "data/main.sqlite"
)

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>htimg login</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root { color-scheme: dark; --bg:#09090b; --card:#111114; --raised:#17171b; --border:#2a2a32; --text:#f4f4f5; --muted:#a1a1aa; --accent:#6366f1; --danger:#ef4444; }
    * { box-sizing: border-box; }
    body { margin:0; min-height:100vh; display:grid; place-items:center; padding:24px; background:radial-gradient(circle at 50% -20%, rgba(99,102,241,.16), transparent 34rem), var(--bg); color:var(--text); font-family:Geist, Inter, ui-sans-serif, system-ui, sans-serif; }
    main { width:min(100%, 380px); border:1px solid var(--border); border-radius:12px; background:var(--card); box-shadow:0 18px 50px rgba(0,0,0,.34); padding:20px; }
    .brand { display:flex; align-items:center; gap:12px; margin-bottom:20px; }
    img { width:36px; height:36px; border-radius:9px; background:#fff; object-fit:cover; }
    h1 { margin:0; font-size:16px; line-height:1.1; }
    p { margin:3px 0 0; color:var(--muted); font-size:13px; }
    form { display:grid; gap:12px; }
    label { display:grid; gap:7px; color:var(--text); font-size:12px; font-weight:650; }
    input { width:100%; height:40px; border:1px solid var(--border); border-radius:10px; padding:0 11px; background:var(--raised); color:var(--text); font:inherit; outline:none; }
    input:focus { border-color:var(--accent); box-shadow:0 0 0 3px rgba(99,102,241,.16); }
    button { height:40px; border:0; border-radius:10px; background:var(--accent); color:#fff; font:inherit; font-weight:650; cursor:pointer; }
    .error { min-height:18px; color:var(--danger); font-size:13px; }
    .loading { color:var(--muted); font-size:12px; margin-top:14px; }
  </style>
</head>
<body>
  <main>
    <div class="brand">
      <img src="/logo.webp" alt="htimg">
      <div><h1>htimg</h1><p>Admin login</p></div>
    </div>
    <form method="post" action="/login">
      <label>Username <input name="username" autocomplete="username" required autofocus></label>
      <label>Password <input name="password" type="password" autocomplete="current-password" required></label>
      <button type="submit">Log in</button>
      <div class="error">{{.Error}}</div>
    </form>
    <div class="loading">Initializing secure session...</div>
  </main>
</body>
</html>`))

type Config struct {
	AdminUsername string
	AdminPassword string
	SessionSecret string
	DBPath        string
}

type Manager struct {
	cfg      Config
	db       *sql.DB
	sessions map[string]time.Time
	mu       sync.Mutex
	now      func() time.Time
}

func Init(envPath string) error {
	if envPath == "" {
		return errors.New("env path is required")
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(envPath); errors.Is(err, os.ErrNotExist) {
		secret, err := randomToken()
		if err != nil {
			return err
		}
		body := fmt.Sprintf("ADMIN_USERNAME=admin\nADMIN_PASSWORD=change-me-now\nSESSION_SECRET=%s\n", secret)
		if err := os.WriteFile(envPath, []byte(body), 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	cfg, err := LoadConfig(envPath)
	if err != nil {
		return err
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	return db.Close()
}

func LoadConfig(envPath string) (Config, error) {
	if envPath == "" {
		return Config{}, errors.New("env path is required")
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return Config{}, fmt.Errorf("read env file %q: %w", envPath, err)
	}

	values := map[string]string{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid env line %d", lineNo+1)
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}

	cfg := Config{
		AdminUsername: values["ADMIN_USERNAME"],
		AdminPassword: values["ADMIN_PASSWORD"],
		SessionSecret: values["SESSION_SECRET"],
		DBPath:        DBPathForEnv(envPath),
	}
	if cfg.AdminUsername == "" || cfg.AdminPassword == "" || cfg.SessionSecret == "" {
		return Config{}, errors.New("env file must define ADMIN_USERNAME, ADMIN_PASSWORD, and SESSION_SECRET")
	}

	return cfg, nil
}

func DBPathForEnv(envPath string) string {
	if envPath == "" {
		return defaultDBPath
	}
	root := filepath.Dir(filepath.Dir(envPath))
	return filepath.Clean(filepath.Join(root, defaultDBPath))
}

func OpenDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func NewManager(cfg Config, db *sql.DB) *Manager {
	return &Manager{
		cfg:      cfg,
		db:       db,
		sessions: map[string]time.Time{},
		now:      time.Now,
	}
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Authenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.renderLogin(w, "")
	case http.MethodPost:
		m.handleLogin(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if sid, ok := m.sessionID(r); ok {
		m.mu.Lock()
		delete(m.sessions, sid)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (m *Manager) Authenticated(r *http.Request) bool {
	sid, ok := m.sessionID(r)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expires, ok := m.sessions[sid]
	if !ok {
		return false
	}
	if !m.now().Before(expires) {
		delete(m.sessions, sid)
		return false
	}
	return true
}

func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ctx := r.Context()
	if blocked, err := m.blocked(ctx, ip); err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	} else if blocked {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if m.validCredentials(username, password) {
		if err := m.createSession(w); err != nil {
			http.Error(w, "login unavailable", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, protectedAppPath, http.StatusSeeOther)
		return
	}

	blocked, err := m.recordFailure(ctx, ip)
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	if blocked {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	m.renderLogin(w, "Invalid username or password.")
}

func (m *Manager) renderLogin(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTemplate.Execute(w, map[string]string{"Error": message})
}

func (m *Manager) validCredentials(username, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(m.cfg.AdminUsername)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(m.cfg.AdminPassword)) == 1
	return userOK && passOK
}

func (m *Manager) createSession(w http.ResponseWriter) error {
	sid, err := randomToken()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions[sid] = m.now().Add(sessionLifetime)
	m.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sid + "." + m.sign(sid),
		Path:     "/",
		Expires:  m.now().Add(sessionLifetime),
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (m *Manager) sessionID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	sid, sig, ok := strings.Cut(cookie.Value, ".")
	if !ok || sid == "" || sig == "" {
		return "", false
	}
	expected := m.sign(sid)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return "", false
	}
	return sid, true
}

func (m *Manager) sign(value string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionSecret))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Manager) blocked(ctx context.Context, ip string) (bool, error) {
	if err := m.purgeOld(ctx); err != nil {
		return false, err
	}
	count, err := m.failureCount(ctx, ip)
	return count >= maxFailures, err
}

func (m *Manager) recordFailure(ctx context.Context, ip string) (bool, error) {
	if err := m.purgeOld(ctx); err != nil {
		return false, err
	}
	if _, err := m.db.ExecContext(ctx, `INSERT INTO login_failures (ip, attempted_at) VALUES (?, ?)`, ip, m.now().Unix()); err != nil {
		return false, err
	}
	count, err := m.failureCount(ctx, ip)
	return count >= maxFailures, err
}

func (m *Manager) purgeOld(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM login_failures WHERE attempted_at < ?`, m.now().Add(-failureWindow).Unix())
	return err
}

func (m *Manager) failureCount(ctx context.Context, ip string) (int, error) {
	var count int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_failures WHERE ip = ? AND attempted_at >= ?`, ip, m.now().Add(-failureWindow).Unix()).Scan(&count)
	return count, err
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS login_failures (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ip TEXT NOT NULL,
  attempted_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_login_failures_ip_time
ON login_failures (ip, attempted_at);`)
	return err
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
