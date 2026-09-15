package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aiusage/internal/model"
)

// Tests use synthetic data only. Constructing a controller must never need
// installed provider clients, real user settings, or conversation directories.
type fakeService struct {
	cfg *model.Config
}

func newFakeService() *fakeService {
	return &fakeService{cfg: &model.Config{
		RefreshSeconds: 1,
		Providers:      []model.ProviderConfig{{ID: model.ProviderCodex}},
	}}
}

func (f *fakeService) Config() *model.Config { return f.cfg }
func (f *fakeService) UpdateConfig(update func(*model.Config)) error {
	update(f.cfg)
	return nil
}
func (f *fakeService) Snapshot() model.Snapshot    { return model.Snapshot{Host: "test-host"} }
func (f *fakeService) Diagnose() []model.Diagnosis { return nil }
func (f *fakeService) Rescan()                     {}
func (f *fakeService) ScanOnce()                   {}

func TestSnapshotRequiresLocalAuthenticatedRequest(t *testing.T) {
	s := New(newFakeService())
	for _, tc := range []struct {
		name, remote, token string
		status              int
	}{
		{"authenticated", "127.0.0.1:1234", s.token, http.StatusOK},
		{"missing token", "127.0.0.1:1234", "", http.StatusForbidden},
		{"remote client", "192.0.2.1:1234", s.token, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/snapshot?t="+tc.token, nil)
			r.RemoteAddr = tc.remote
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				var snapshot model.Snapshot
				if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.Host != "test-host" {
					t.Fatalf("snapshot=%+v error=%v", snapshot, err)
				}
			}
		})
	}
}

func TestLimitDelegatesToService(t *testing.T) {
	f := newFakeService()
	s := New(f)
	r := httptest.NewRequest(http.MethodPost, "/api/limit?t="+s.token, strings.NewReader(`{"provider":"codex","limit":100,"used":25,"window_hours":5}`))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	p := f.cfg.Provider(model.ProviderCodex)
	if p.Limit != 100 || p.WindowSeconds != 18000 || p.Manual == nil || p.Manual.Used != 25 {
		t.Fatalf("configuration not forwarded: %+v", p)
	}
}

func TestPanelAssetsAreServed(t *testing.T) {
	s := New(newFakeService())
	for _, path := range []string{"/?t=" + s.token, "/robot-icon.png", "/favicon.ico"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Errorf("asset=%q status=%d bytes=%d", path, w.Code, w.Body.Len())
		}
	}
}

type fakeWindow struct{ drags int }

func (f *fakeWindow) BeginDrag() { f.drags++ }

func TestWindowDragIsGuardedAndOptional(t *testing.T) {
	post := func(s *Server, path, token, remote string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path+"?t="+token, nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}

	s := New(newFakeService())
	window := &fakeWindow{}
	s.SetPanelWindow(window)

	const drag = "/api/window/drag"
	if code := post(s, drag, s.token, "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("authenticated status=%d", code)
	}
	if code := post(s, drag, "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Errorf("missing token status=%d", code)
	}
	if code := post(s, drag, s.token, "192.0.2.1:1234").Code; code != http.StatusForbidden {
		t.Errorf("remote client status=%d", code)
	}
	if window.drags != 1 {
		t.Fatalf("拖曳觸發次數不對：%d", window.drags)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/window/drag?t="+s.token, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status=%d", w.Code)
	}

	bare := New(newFakeService())
	got := post(bare, drag, bare.token, "127.0.0.1:1234")
	if got.Code != http.StatusOK {
		t.Fatalf("no window status=%d", got.Code)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil || body.OK {
		t.Fatalf("no window body=%s error=%v", got.Body.String(), err)
	}
}
