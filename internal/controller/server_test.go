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
