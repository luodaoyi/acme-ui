package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/acme-ui/acme-ui/internal/acme"
	"github.com/acme-ui/acme-ui/internal/actions"
	"github.com/acme-ui/acme-ui/internal/auth"
	"github.com/acme-ui/acme-ui/internal/jobs"
	"github.com/acme-ui/acme-ui/internal/ui"
)

type Config struct {
	Bind      string
	Port      int
	Version   string
	Commit    string
	MasterKey string
	Locator   *acme.Locator
	Jobs      *jobs.Store
}

type Server struct {
	cfg      Config
	sessions *auth.SessionStore
	limiter  *auth.LoginLimiter
}

func New(cfg Config) *Server {
	return &Server{
		cfg:      cfg,
		sessions: auth.NewSessionStore(6 * time.Hour),
		limiter:  auth.NewLoginLimiter(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.withAuth(s.handleLogout))
	mux.HandleFunc("/api/status", s.withAuth(s.handleStatus))
	mux.HandleFunc("/api/acme/install", s.withAuth(s.handleInstallAcme))
	mux.HandleFunc("/api/certs", s.withAuth(s.handleCerts))
	mux.HandleFunc("/api/cert/info", s.withAuth(s.handleCertInfo))
	mux.HandleFunc("/api/issue", s.withAuth(s.handleIssue))
	mux.HandleFunc("/api/install", s.withAuth(s.handleInstall))
	mux.HandleFunc("/api/renew", s.withAuth(s.handleRenew))
	mux.HandleFunc("/api/revoke", s.withAuth(s.handleRevoke))
	mux.HandleFunc("/api/remove", s.withAuth(s.handleRemove))
	mux.HandleFunc("/api/uninstall", s.withAuth(s.handleUninstall))
	mux.HandleFunc("/api/jobs", s.withAuth(s.handleJobs))
	mux.HandleFunc("/api/jobs/", s.withAuth(s.handleJobByID))
	static, _ := fs.Sub(ui.Files, ".")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := ui.Files.ReadFile("index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, ok := s.sessions.FromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"csrf":          session.CSRF,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ip := auth.ClientIP(r)
	if !s.limiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many login failures; retry later")
		return
	}
	var req struct {
		MasterKey string `json:"masterKey"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok := subtle.ConstantTimeCompare([]byte(req.MasterKey), []byte(s.cfg.MasterKey)) == 1 && len(req.MasterKey) == len(s.cfg.MasterKey)
	s.limiter.Record(ip, ok)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid masterKey")
		return
	}
	session, err := s.sessions.Create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((6 * time.Hour).Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"csrf":          session.CSRF,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, session *auth.Session) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.sessions.Delete(session.Token)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	resolved := s.cfg.Locator.Resolve()
	versionOutput := ""
	versionExitCode := 0
	versionError := ""
	if resolved.Found {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, code, err := acme.VersionOutput(ctx, resolved)
		versionOutput = strings.TrimSpace(out)
		versionExitCode = code
		if err != nil {
			versionError = err.Error()
		}
	}
	currentUser := map[string]string{}
	if u, err := user.Current(); err == nil {
		currentUser["uid"] = u.Uid
		currentUser["gid"] = u.Gid
		currentUser["username"] = u.Username
		currentUser["home"] = u.HomeDir
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         s.cfg.Version,
		"commit":          s.cfg.Commit,
		"bind":            s.cfg.Bind,
		"port":            s.cfg.Port,
		"acme":            resolved,
		"acmeVersion":     versionOutput,
		"acmeVersionCode": versionExitCode,
		"acmeVersionErr":  versionError,
		"tools":           acme.ToolStatus("nginx", "haproxy", "systemctl", "curl", "bash"),
		"user":            currentUser,
		"home":            os.Getenv("HOME"),
	})
}

func (s *Server) handleInstallAcme(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req actions.InstallAcmeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := actions.BuildInstallAcme(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot, err := s.cfg.Jobs.Start("install-acme.sh", plan.Preview, func(ctx context.Context, emit func(stream, text string)) (int, error) {
		return actions.RunInstallAcme(ctx, plan, emit)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

func (s *Server) handleCerts(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	spec := acme.BuildList()
	out, code, err := s.runSync(r.Context(), spec)
	writeJSON(w, http.StatusOK, map[string]any{
		"output":   out,
		"exitCode": code,
		"error":    errString(err),
	})
}

func (s *Server) handleCertInfo(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	req := acme.DomainRequest{
		Domain: r.URL.Query().Get("domain"),
		ECC:    r.URL.Query().Get("ecc") == "1" || r.URL.Query().Get("ecc") == "true",
	}
	spec, err := acme.BuildInfo(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, code, err := s.runSync(r.Context(), spec)
	writeJSON(w, http.StatusOK, map[string]any{
		"output":   out,
		"exitCode": code,
		"error":    errString(err),
	})
}

func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req acme.IssueRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	spec, err := acme.BuildIssue(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.startJob(w, spec)
}

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req acme.InstallRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	spec, err := acme.BuildInstall(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.startJob(w, spec)
}

func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	s.handleDomainJob(w, r, acme.BuildRenew)
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	s.handleDomainJob(w, r, acme.BuildRevoke)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	s.handleDomainJob(w, r, acme.BuildRemove)
}

func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req actions.UninstallRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := actions.BuildUninstall(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot, err := s.cfg.Jobs.Start("uninstall", plan.Preview, func(ctx context.Context, emit func(stream, text string)) (int, error) {
		return actions.RunUninstall(ctx, plan, emit)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

func (s *Server) handleDomainJob(w http.ResponseWriter, r *http.Request, build func(acme.DomainRequest) (acme.CommandSpec, error)) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req acme.DomainRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	spec, err := build(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.startJob(w, spec)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.cfg.Jobs.List()})
}

func (s *Server) handleJobByID(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		snapshot, ok := s.cfg.Jobs.Get(id, true)
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
		return
	}
	switch parts[1] {
	case "events":
		s.handleJobEvents(w, r, id)
	case "cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !s.cfg.Jobs.Cancel(id) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	snapshot, ok := s.cfg.Jobs.Get(id, true)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeSSE(w, "state", snapshot)
	for _, entry := range snapshot.Logs {
		writeSSE(w, "log", entry)
	}
	flusher.Flush()

	ch, unsubscribe, ok := s.cfg.Jobs.Subscribe(id)
	if !ok {
		return
	}
	defer unsubscribe()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, event.Type, event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) startJob(w http.ResponseWriter, spec acme.CommandSpec) {
	resolved := s.cfg.Locator.Resolve()
	if !resolved.Found {
		writeError(w, http.StatusBadRequest, "acme.sh not found")
		return
	}
	args := s.cfg.Locator.WithHome(spec.Args)
	preview := acme.Preview(resolved.Path, args, spec.Env)
	snapshot, err := s.cfg.Jobs.Start(spec.Kind, preview, func(ctx context.Context, emit func(stream, text string)) (int, error) {
		return acme.Run(ctx, resolved, args, spec.Env, spec.Secrets, emit)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

func (s *Server) runSync(ctx context.Context, spec acme.CommandSpec) (string, int, error) {
	resolved := s.cfg.Locator.Resolve()
	args := s.cfg.Locator.WithHome(spec.Args)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return acme.RunSync(ctx, resolved, args, spec.Env, spec.Secrets)
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, *auth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.sessions.FromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if needsCSRF(r.Method) {
			token := r.Header.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRF)) != 1 || len(token) != len(session.CSRF) {
				writeError(w, http.StatusForbidden, "invalid csrf token")
				return
			}
		}
		next(w, r, session)
	}
}

func needsCSRF(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
