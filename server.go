package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed ui.html
var uiFS embed.FS

type Server struct {
	store *Store
	token string
	mux   *http.ServeMux
}

func NewServer(store *Store) *Server {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := &Server{store: store, token: hex.EncodeToString(b), mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.guard(s.handleIndex))
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

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := uiFS.ReadFile("ui.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
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
	err := s.store.UpdateConfig(func(c *Config) {
		p := c.provider(req.Provider)
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
				p.Manual = &ManualUsage{Used: *req.Used, Limit: req.Limit, AsOf: time.Now()}
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
	return url, ln.Close, nil
}
