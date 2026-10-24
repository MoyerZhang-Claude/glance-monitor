// Package web serves the settings UI on 127.0.0.1 with a per-run token.
// It is platform independent; the app plugs in through Backend.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"glance/internal/config"
	"glance/internal/data"
)

//go:embed assets
var assets embed.FS

// Backend is implemented by the application.
type Backend interface {
	Config() *config.Config
	SetConfig(c *config.Config) (*config.Config, error)
	Snapshot() *data.Snapshot
	Action(name, arg string) (any, error)
	Meta() map[string]any  // fonts, monitors, version... (slow-changing)
	State() map[string]any // autostart, editing, hotkey errors... (fast-changing)
}

type Server struct {
	b        Backend
	ln       net.Listener
	token    string
	host     string
	mu       sync.Mutex
	lastSeen time.Time
}

// Start listens on a random loopback port.
func Start(b Backend) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 16)
	rand.Read(tok)
	s := &Server{b: b, ln: ln, token: hex.EncodeToString(tok), host: ln.Addr().String()}
	go http.Serve(ln, s.routes())
	return s, nil
}

// URL is the address to open (includes the token).
func (s *Server) URL() string { return "http://" + s.host + "/?t=" + s.token }

// Active reports whether a settings page polled us recently.
func (s *Server) Active(within time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastSeen) < within
}

func (s *Server) Close() { s.ln.Close() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(assets, "assets")
	static := http.FileServer(http.FS(sub))
	mux.Handle("/", static)
	mux.HandleFunc("/api/state", s.api(s.handleState))
	mux.HandleFunc("/api/config", s.api(s.handleConfig))
	mux.HandleFunc("/api/meta", s.api(s.handleMeta))
	mux.HandleFunc("/api/action", s.api(s.handleAction))
	return s.guard(mux)
}

// guard blocks DNS-rebinding (Host must be our loopback address) and
// disables caching.
func (s *Server) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) api(fn func(http.ResponseWriter, *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Glance-Token") != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "bad content type", http.StatusUnsupportedMediaType)
			return
		}
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
		res, err := fn(w, r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(res)
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) (any, error) {
	return map[string]any{"snapshot": SnapshotJSON(s.b.Snapshot()), "app": s.b.State()}, nil
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) (any, error) {
	switch r.Method {
	case http.MethodGet:
		return s.b.Config(), nil
	case http.MethodPost:
		var c config.Config
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&c); err != nil {
			return nil, err
		}
		return s.b.SetConfig(&c)
	}
	return nil, errors.New("method not allowed")
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) (any, error) {
	m := s.b.Meta()
	m["catalog"] = data.Catalog
	m["groups"] = data.Groups
	m["defaults"] = config.Default()
	return m, nil
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return nil, errors.New("method not allowed")
	}
	var req struct {
		Action string `json:"action"`
		Arg    string `json:"arg"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		return nil, err
	}
	log.Printf("settings action: %s %s", req.Action, req.Arg)
	return s.b.Action(req.Action, req.Arg)
}

// SnapshotJSON converts a snapshot for the settings page.
func SnapshotJSON(s *data.Snapshot) map[string]any {
	vals := map[string]any{}
	for k, v := range s.Values {
		if v.OK {
			vals[k] = round(v.V)
		}
	}
	readings := make([]data.Reading, len(s.Readings))
	for i, r := range s.Readings {
		r.Value = round(r.Value)
		readings[i] = r
	}
	return map[string]any{
		"time":       s.Time.UnixMilli(),
		"values":     vals,
		"cpuName":    s.CPUName,
		"gpuName":    s.GPUName,
		"gpuAsleep":  s.GPUAsleep,
		"fpsActive":  s.FPSActive,
		"fpsProcess": s.FPSProcess,
		"fpsApi":     s.FPSAPI,
		"frameGen":   s.FrameGen,
		"unsupported": s.Unsupported,
		"frameTimes": s.FrameTimes,
		"readings":   readings,
		"status":     s.Status,
	}
}

func round(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	return f
}
