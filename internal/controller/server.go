package controller

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"aiusage/internal/model"
	"aiusage/internal/view"
)

// UsageService is the controller's application contract. Storage and collectors
// stay behind this boundary so HTTP handlers can be tested without local data.
type UsageService interface {
	Config() *model.Config
	UpdateConfig(func(*model.Config)) error
	Snapshot() model.Snapshot
	Diagnose() []model.Diagnosis
	Rescan()
	ScanOnce()
}
type Server struct {
	store   UsageService
	token   string
	mux     *http.ServeMux
	windows panelWindows
}

// An SSE connection belongs to one panel window. Refresh/reconnect gets a
// grace period; minimizing does not disconnect. Never attach API polling here.
type panelWindows struct {
	mu     sync.Mutex
	count  int
	timer  *time.Timer
	done   chan struct{}
	closed bool
}

func (p *panelWindows) attach() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	if p.timer != nil {
		p.timer.Stop()
	}
}

func (p *panelWindows) detach(grace time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count--
	if p.count != 0 || p.done == nil || p.closed {
		return
	}
	p.timer = time.AfterFunc(grace, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.count == 0 && !p.closed {
			p.closed = true
			close(p.done)
		}
	})
}

func New(store UsageService) *Server {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := &Server{store: store, token: hex.EncodeToString(b), mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Static, non-sensitive favicon: browser icon requests may omit cookies.
	s.mux.HandleFunc("/robot-icon.png", view.ServeRobotIcon)
	s.mux.HandleFunc("/favicon.ico", view.ServeRobotIcon)
	s.mux.HandleFunc("/", s.guard(view.ServeIndex))
	s.mux.HandleFunc("/api/snapshot", s.guard(s.handleSnapshot))
	s.mux.HandleFunc("/api/stream", s.guard(s.handleStream))
	s.mux.HandleFunc("/api/limit", s.guard(s.handleLimit))
	s.mux.HandleFunc("/api/rescan", s.guard(s.handleRescan))
	s.mux.HandleFunc("/api/doctor", s.guard(s.handleDoctor))
}

// guard 只做兩件事：確認來自本機、確認帶對 token。
// 面板綁在 127.0.0.1，token 防止本機其他網頁順手打這個埠。
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			http.Error(w, "僅接受本機連線", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !strings.Contains(o, r.Host) {
			http.Error(w, "來源不符", http.StatusForbidden)
			return
		}
		ok := false
		if c, err := r.Cookie("aiusage"); err == nil && c.Value == s.token {
			ok = true
		}
		if t := r.URL.Query().Get("t"); t == s.token {
			ok = true
			http.SetCookie(w, &http.Cookie{
				Name: "aiusage", Value: s.token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
		}
		if !ok {
			http.Error(w, "請用啟動時印出的網址開啟面板", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Snapshot())
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "不支援串流", 500)
		return
	}
	s.windows.attach()
	defer s.windows.detach(8 * time.Second)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	interval := time.Duration(s.store.Config().RefreshSeconds) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	send := func() bool {
		data, err := json.Marshal(s.store.Snapshot())
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !send() {
				return
			}
		}
	}
}

type limitReq struct {
	Provider string   `json:"provider"`
	Limit    float64  `json:"limit"`
	Used     *float64 `json:"used,omitempty"`
	Window   *float64 `json:"window_hours,omitempty"`
}

func (s *Server) handleLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
	var req limitReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "請求格式錯誤", http.StatusBadRequest)
		return
	}
	err := s.store.UpdateConfig(func(c *model.Config) {
		p := c.Provider(req.Provider)
		if p == nil {
			return
		}
		p.Limit = req.Limit
		if req.Window != nil && *req.Window > 0 {
			p.WindowSeconds = int64(*req.Window * 3600)
		}
		if req.Used != nil {
			if req.Limit <= 0 {
				p.Manual = nil
			} else {
				p.Manual = &model.ManualUsage{Used: *req.Used, Limit: req.Limit, AsOf: time.Now()}
			}
		}
	})
	if err != nil {
		http.Error(w, "設定寫入失敗："+err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
	// Rescan 會等目前這輪掃描結束，所以整包丟到背景，不要卡住這個請求。
	go func() {
		s.store.Rescan()
		s.store.ScanOnce()
	}()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Diagnose())
}

// Serve 綁本機埠並回傳實際使用的網址。
func (s *Server) Serve(port int) (string, func() error, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", addr.Port, s.token)

	srv := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("面板停止:", err)
		}
	}()
	return url, srv.Close, nil
}

// EnableWindowExit makes Done close after the last panel disconnects.
// Call it before Serve; headless mode leaves Done nil and runs until interrupted.
func (s *Server) EnableWindowExit() { s.windows.done = make(chan struct{}) }

func (s *Server) Done() <-chan struct{} { return s.windows.done }
