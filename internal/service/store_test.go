package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"aiusage/internal/model"
)

type fakeCollector struct {
	batches       []model.Collection
	calls, resets int
	policy        model.ProviderPolicy
}

func (f *fakeCollector) Collect(model.ProviderConfig, time.Time, bool) model.Collection {
	index := f.calls
	f.calls++
	if len(f.batches) == 0 {
		return model.Collection{}
	}
	if index >= len(f.batches) {
		index = len(f.batches) - 1
	}
	return f.batches[index]
}
func (f *fakeCollector) Reset()                       { f.calls = 0; f.resets++ }
func (f *fakeCollector) Policy() model.ProviderPolicy { return f.policy }

type fakeRepository struct {
	saved *model.Config
	err   error
}

func (r *fakeRepository) Save(c *model.Config) error { r.saved = c; return r.err }

func testConfig() *model.Config {
	return &model.Config{RefreshSeconds: 3, RetentionDays: 40, WarnRatio: .8, PreferReported: true,
		Providers: []model.ProviderConfig{{ID: "test", Name: "Test", Enabled: true, Metric: "tokens", WindowKind: "rolling", WindowSeconds: 18000}}}
}

func testStore(collector *fakeCollector) *Store {
	return New(testConfig(), nil, map[string]Collector{"test": collector}, nil)
}

// Exercise collection through the service port rather than reproducing its
// internal de-duplication algorithm in the test.
func TestCumulativeDeltaInStore(t *testing.T) {
	now := time.Now()
	event := func(in, out, cache, total int64, key string) model.Event {
		return model.Event{Provider: "test", At: now, Session: "session", Cumulative: true, Key: key,
			Usage: model.Usage{Input: in, Output: out, CacheRead: cache, Total: total, UseTotal: true}}
	}
	a := event(1000, 200, 800, 1200, "a")
	b := event(1500, 260, 1200, 1760, "b")
	c := event(1500, 260, 1200, 1760, "c")
	collector := &fakeCollector{batches: []model.Collection{
		{Events: []model.Event{a}, Stats: model.ScanStats{Files: 1, RootsHit: []string{"logs"}}},
		{Events: []model.Event{a, b, c}, Stats: model.ScanStats{Files: 1, RootsHit: []string{"logs"}}},
	}}
	s := testStore(collector)
	s.ScanOnce()
	if got := s.Snapshot().Providers[0].Used; got != 1200 {
		t.Fatalf("first total = %v", got)
	}
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if p.Used != 1760 || p.Requests != 2 {
		t.Fatalf("duplicated/cumulative usage: %+v", p)
	}
	s.Rescan()
	s.ScanOnce()
	if got := s.Snapshot().Providers[0].Used; got != 1200 || collector.resets != 1 {
		t.Fatalf("rescan total=%v resets=%d", got, collector.resets)
	}
}

func TestWindowBounds(t *testing.T) {
	now := time.Date(2026, 9, 11, 14, 30, 0, 0, time.Local)
	start, end, label := windowBounds(model.ProviderConfig{WindowKind: "rolling", WindowSeconds: 18000}, now)
	if now.Sub(start) != 5*time.Hour || !end.IsZero() || label != "5 小時滾動視窗" {
		t.Fatalf("rolling: %v %v %q", start, end, label)
	}
	start, end, _ = windowBounds(model.ProviderConfig{WindowKind: "day"}, now)
	if start.Hour() != 0 || end.Day() != 12 {
		t.Fatalf("day: %v %v", start, end)
	}
	start, end, _ = windowBounds(model.ProviderConfig{WindowKind: "week"}, now)
	if start.Weekday() != time.Monday || end.Sub(start) != 7*24*time.Hour {
		t.Fatalf("week: %v %v", start, end)
	}
	start, end, _ = windowBounds(model.ProviderConfig{WindowKind: "month"}, now)
	if start.Day() != 1 || end.Month() != time.October {
		t.Fatalf("month: %v %v", start, end)
	}
}

func TestReportedResetBeatsRollingGuess(t *testing.T) {
	now := time.Now()
	reset := now.Add(2 * time.Hour)
	percent := 93.0
	collector := &fakeCollector{batches: []model.Collection{{
		Reported: &model.Reported{At: now, PercentUsed: &percent, ResetAt: &reset, Windows: []model.ReportedWindow{{Label: "5 小時", PercentUsed: 93, ResetAt: &reset}}},
		Events:   []model.Event{{At: now.Add(-4 * time.Hour), Usage: model.Usage{Input: 100, Output: 50}, Key: "k1"}},
	}}}
	s := testStore(collector)
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if !p.HasLimit || p.Percent != .93 || p.ResetLabel != "重置" || p.ResetAt == nil || *p.ResetAt != reset.Unix() || p.WindowLabel != "5 小時窗" {
		t.Fatalf("reported reset lost: %+v", p)
	}
}

func TestClaudeQuotaWithoutTokenLogs(t *testing.T) {
	zero := 0.0
	s := testStore(&fakeCollector{batches: []model.Collection{{Reported: &model.Reported{At: time.Now(), PercentUsed: &zero, Source: "Claude Desktop 額度快照"}}}})
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if p.Status != "ok" || !p.HasLimit || p.QuotaAt == nil || p.Percent != 0 {
		t.Fatalf("quota-only: %+v", p)
	}
}

func TestConfigIsolationAndSaveFailure(t *testing.T) {
	repository := &fakeRepository{}
	initial := testConfig()
	s := New(initial, repository, nil, nil)
	initial.Providers[0].Name = "mutated input"
	copy := s.Config()
	copy.Providers[0].Name = "mutated output"
	if s.Config().Providers[0].Name != "Test" {
		t.Fatal("configuration aliases caller memory")
	}
	repository.err = errors.New("disk full")
	if err := s.UpdateConfig(func(c *model.Config) { c.Providers[0].Limit = 99 }); err == nil {
		t.Fatal("missing save error")
	}
	if s.Config().Providers[0].Limit != 0 {
		t.Fatal("failed write changed running config")
	}
	repository.err = nil
	var captured *model.Config
	if err := s.UpdateConfig(func(c *model.Config) { c.Providers[0].Limit = 42; captured = c }); err != nil {
		t.Fatal(err)
	}
	captured.Providers[0].Limit = 100
	repository.saved.Providers[0].Limit = 200
	if s.Config().Providers[0].Limit != 42 {
		t.Fatal("saved settings remain aliased")
	}
}

func TestCollectorRegistryAndDisabledSources(t *testing.T) {
	cfg := testConfig()
	cfg.Providers = append(cfg.Providers, model.ProviderConfig{ID: "disabled", Enabled: false})
	active, disabled := &fakeCollector{}, &fakeCollector{}
	s := New(cfg, nil, map[string]Collector{"test": active, "disabled": disabled}, func() map[string]bool { return map[string]bool{"test": true} })
	s.ScanOnce()
	if active.calls != 1 || disabled.calls != 0 {
		t.Fatal("disabled source was collected")
	}
	if got := s.Snapshot().Providers; len(got) != 1 || got[0].ID != "test" {
		t.Fatalf("installation filter: %+v", got)
	}
}

func TestSourcePolicyWithoutProviderSwitch(t *testing.T) {
	percent := 24.0
	s := testStore(&fakeCollector{policy: model.ProviderPolicy{Name: "Custom quota", QuotaOnly: true}, batches: []model.Collection{{Reported: &model.Reported{At: time.Now(), PercentUsed: &percent}}}})
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if p.Name != "Custom quota" || p.Metric != "quota" || !p.HasLimit || p.ShowLimit || p.Percent != .24 {
		t.Fatalf("injected policy: %+v", p)
	}
}

func TestConcurrentScanRescanAndSettings(t *testing.T) {
	s := testStore(&fakeCollector{})
	var group sync.WaitGroup
	for task := 0; task < 4; task++ {
		group.Add(1)
		go func(task int) {
			defer group.Done()
			for i := 0; i < 30; i++ {
				switch task {
				case 0:
					s.ScanOnce()
				case 1:
					s.Rescan()
				case 2:
					s.Snapshot()
					s.Diagnose()
				default:
					s.UpdateConfig(func(c *model.Config) { c.Providers[0].Limit = float64(i) })
				}
			}
		}(task)
	}
	group.Wait()
}
