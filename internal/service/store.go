// Package service coordinates collection and usage calculations. External
// systems are supplied through small interfaces at the application boundary.
package service

import (
	"fmt"
	"os"
	"sync"
	"time"

	"aiusage/internal/model"
)

// Collector returns newly read events and the latest quota/diagnostic state.
// Calls are serialized by Store; Reset must discard incremental read state.
// Implementations must not mutate a Collection after returning it.
type Collector interface {
	Collect(model.ProviderConfig, time.Time, bool) model.Collection
	Reset()
}

// ConfigRepository persists settings without coupling calculations to files.
type ConfigRepository interface {
	Save(*model.Config) error
}

type Store struct {
	mu          sync.RWMutex
	scanMu      sync.Mutex
	cfg         *model.Config
	repository  ConfigRepository
	collectors  map[string]Collector
	installed   func() map[string]bool
	now         func() time.Time
	events      map[string][]model.Event
	seen        map[string]struct{}
	cum         map[string]model.Usage
	reported    map[string]*model.Reported
	stats       map[string]*model.ScanStats
	diagnoses   map[string]model.Diagnosis
	collectKeys bool
	host        string
	scanDur     time.Duration
}

func New(cfg *model.Config, repository ConfigRepository, collectors map[string]Collector, installed func() map[string]bool) *Store {
	host, _ := os.Hostname()
	registry := make(map[string]Collector, len(collectors))
	for id, collector := range collectors {
		registry[id] = collector
	}
	s := &Store{cfg: cfg.Clone(), repository: repository, collectors: registry,
		installed: installed, now: time.Now, host: host}
	s.clear()
	return s
}

func (s *Store) clear() {
	s.events = map[string][]model.Event{}
	s.seen = map[string]struct{}{}
	s.cum = map[string]model.Usage{}
	s.reported = map[string]*model.Reported{}
	s.stats = map[string]*model.ScanStats{}
	s.diagnoses = map[string]model.Diagnosis{}
}

// Config returns a detached value so callers cannot mutate running settings.
func (s *Store) Config() *model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

// UpdateConfig publishes a change only after it has been persisted successfully.
func (s *Store) UpdateConfig(fn func(*model.Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Clone()
	fn(next)
	if s.repository != nil {
		if err := s.repository.Save(next.Clone()); err != nil {
			return err
		}
	}
	s.cfg = next.Clone()
	return nil
}

func (s *Store) SetCollectKeys(enabled bool) {
	s.mu.Lock()
	s.collectKeys = enabled
	s.mu.Unlock()
}

func (s *Store) Rescan() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	for _, collector := range s.collectors {
		collector.Reset()
	}
	s.mu.Lock()
	s.clear()
	s.mu.Unlock()
}

func (s *Store) ScanOnce() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := s.now()
	s.mu.RLock()
	cfg, keys := s.cfg.Clone(), s.collectKeys
	s.mu.RUnlock()
	cutoff := start.AddDate(0, 0, -cfg.RetentionDays)
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}
		collector := s.collectors[provider.ID]
		var batch model.Collection
		if collector == nil {
			batch.Stats.Errors = []string{fmt.Sprintf("尚未註冊來源 %s", provider.ID)}
		} else {
			batch = collector.Collect(provider, cutoff, keys)
		}
		s.mu.Lock()
		s.apply(provider, batch, cutoff)
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.scanDur = s.now().Sub(start)
	s.prune(cutoff)
	s.mu.Unlock()
}

func (s *Store) apply(provider model.ProviderConfig, batch model.Collection, cutoff time.Time) {
	id := provider.ID
	if batch.ClearReported {
		delete(s.reported, id)
	}
	if report := batch.Reported; report != nil && report.Usable() {
		if current := s.reported[id]; current == nil || !report.At.Before(current.At) {
			s.reported[id] = report
		}
	}
	stats := batch.Stats
	stats.RootsHit = append([]string(nil), stats.RootsHit...)
	stats.Errors = append([]string(nil), stats.Errors...)
	stats.Events = 0
	for _, event := range batch.Events {
		if _, duplicate := s.seen[event.Key]; duplicate {
			stats.Skipped++
			continue
		}
		s.seen[event.Key] = struct{}{}
		if event.Cumulative {
			key := id + "|" + event.Session
			previous := s.cum[key]
			s.cum[key] = maxUsage(previous, event.Usage)
			event.Usage = deltaUsage(previous, event.Usage)
			if event.Usage.Empty() {
				continue
			}
		}
		if event.At.Before(cutoff) {
			continue
		}
		s.events[id] = append(s.events[id], event)
		stats.Events++
	}
	s.stats[id] = &stats
	diagnosis := cloneDiagnosis(batch.Diagnosis)
	diagnosis.Provider = id
	if diagnosis.Roots == nil {
		diagnosis.Roots = append([]string(nil), provider.Roots...)
	}
	diagnosis.Files, diagnosis.Lines, diagnosis.Events = stats.Files, stats.Lines, stats.Events
	diagnosis.RootsHit, diagnosis.Errors = append([]string(nil), stats.RootsHit...), append([]string(nil), stats.Errors...)
	s.diagnoses[id] = diagnosis
}

func deltaUsage(previous, current model.Usage) model.Usage {
	delta := func(a, b int64) int64 {
		if b > a {
			return b - a
		}
		return 0
	}
	return model.Usage{UseTotal: current.UseTotal, Tool: delta(previous.Tool, current.Tool),
		Input: delta(previous.Input, current.Input), Output: delta(previous.Output, current.Output),
		CacheWrite: delta(previous.CacheWrite, current.CacheWrite), CacheRead: delta(previous.CacheRead, current.CacheRead),
		Reasoning: delta(previous.Reasoning, current.Reasoning), Total: delta(previous.Total, current.Total)}
}

func maxUsage(a, b model.Usage) model.Usage {
	return model.Usage{UseTotal: a.UseTotal || b.UseTotal, Tool: max(a.Tool, b.Tool),
		Input: max(a.Input, b.Input), Output: max(a.Output, b.Output),
		CacheWrite: max(a.CacheWrite, b.CacheWrite), CacheRead: max(a.CacheRead, b.CacheRead),
		Reasoning: max(a.Reasoning, b.Reasoning), Total: max(a.Total, b.Total)}
}

func (s *Store) prune(cutoff time.Time) {
	for id, events := range s.events {
		keep := events[:0]
		for _, event := range events {
			if !event.At.Before(cutoff) {
				keep = append(keep, event)
			}
		}
		s.events[id] = keep
	}
	if len(s.seen) > 400000 {
		s.seen = map[string]struct{}{}
	}
}

func cloneDiagnosis(d model.Diagnosis) model.Diagnosis {
	d.Roots = append([]string(nil), d.Roots...)
	d.RootsHit = append([]string(nil), d.RootsHit...)
	d.DesktopDirs = append([]string(nil), d.DesktopDirs...)
	d.Errors = append([]string(nil), d.Errors...)
	d.TokenKeys = append([]string(nil), d.TokenKeys...)
	if d.QuotaKeys != nil {
		keys := make(map[string]any, len(d.QuotaKeys))
		for key, count := range d.QuotaKeys {
			keys[key] = count
		}
		d.QuotaKeys = keys
	}
	d.Sample = nil
	return d
}

func (s *Store) Diagnose() []model.Diagnosis {
	snapshot := s.Snapshot()
	samples := map[string]model.ProviderSnapshot{}
	for _, sample := range snapshot.Providers {
		samples[sample.ID] = sample
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []model.Diagnosis
	for _, provider := range s.cfg.Providers {
		d := cloneDiagnosis(s.diagnoses[provider.ID])
		d.Provider = provider.ID
		if d.Roots == nil {
			d.Roots = append([]string(nil), provider.Roots...)
		}
		if report := s.reported[provider.ID]; report != nil {
			if d.QuotaKeys == nil {
				d.QuotaKeys = map[string]any{}
			}
			d.QuotaKeys["_reported_usable"] = report.Usable()
		}
		if sample, ok := samples[provider.ID]; ok {
			d.Sample = &sample
		}
		result = append(result, d)
	}
	return result
}
