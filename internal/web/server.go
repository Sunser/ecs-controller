package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"ecs-controller/internal/applog"
	"ecs-controller/internal/monitor"
)

type Server struct {
	service   *monitor.Service
	staticDir string
	mux       *http.ServeMux
}

func NewServer(service *monitor.Service, staticDir string) *Server {
	if staticDir == "" {
		staticDir = "web"
	}
	server := &Server{service: service, staticDir: staticDir, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.Handle("/assets/", http.StripPrefix("/assets/", noCache(http.FileServer(http.Dir(filepath.Join(s.staticDir, "assets"))))))
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/settings", s.handleSettings)
	s.mux.HandleFunc("/api/logs", s.handleLogs)
	s.mux.HandleFunc("/api/instances/", s.handleInstanceAction)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "登录密码无效"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": s.service.Snapshot(),
		"settings": s.service.Settings(),
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "登录密码无效"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Settings())
	case http.MethodPut:
		var update monitor.SettingsView
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		if err := s.service.UpdateSettings(update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.service.Settings())
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "登录密码无效"})
		return
	}
	limit := 120
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": applog.Snapshot(limit)})
}

func (s *Server) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "登录密码无效"})
		return
	}

	instanceID, action, ok := parseInstanceAction(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.service.Config().Server.RequestTimeout)
	defer cancel()

	var err error
	switch action {
	case "start":
		err = s.service.ManualStart(ctx, instanceID)
	case "stop":
		var request struct {
			StopMode  string `json:"stop_mode"`
			PauseMode string `json:"pause_mode"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request)
		}
		err = s.service.ManualStop(ctx, instanceID, request.StopMode, request.PauseMode)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) authorized(r *http.Request) bool {
	expected := s.service.Config().Server.Password
	if expected == "" {
		return false
	}
	actual := r.Header.Get("X-Login-Password")
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func parseInstanceAction(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/instances/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	instanceID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", false
	}
	return instanceID, parts[1], true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
