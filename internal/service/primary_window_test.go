package service

import (
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestFixedPrimaryWindowKeepsWeeklyQuotaInDetails(t *testing.T) {
	now := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
	shortReset, weekReset := now.Add(time.Hour), now.Add(24*time.Hour)
	zero, weekly := 0.0, 40.0
	for _, tc := range []struct {
		name    string
		short   bool
		advance time.Duration
		known   bool
		weekly  bool
	}{
		{"fresh zero is five-hour usage", true, 0, true, true},
		{"expired primary does not promote weekly", true, 2 * time.Hour, false, true},
		{"weekly only stays in details", false, 0, false, true},
		{"all windows expired", true, 25 * time.Hour, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &model.Reported{At: now, Source: "synthetic", PercentUsed: &weekly,
				Windows: []model.ReportedWindow{{Label: "7 天", WindowMinutes: 10080, PercentUsed: weekly, HasPercent: true, ResetAt: &weekReset}}}
			if tc.short {
				r.PercentUsed, r.ResetAt = &zero, &shortReset
				r.Windows = append([]model.ReportedWindow{{Label: "5 小時", WindowMinutes: 300, PercentUsed: zero, HasPercent: true, ResetAt: &shortReset}}, r.Windows...)
			}
			c := &fakeCollector{policy: model.ProviderPolicy{DropExpiredWindows: true, PrimaryWindowMinutes: 300, PrimaryWindowLabel: "5 小時"},
				batches: []model.Collection{{Reported: r}}}
			s := testStore(c)
			s.now = func() time.Time { return now }
			s.ScanOnce()
			s.now = func() time.Time { return now.Add(tc.advance) }
			p := s.Snapshot().Providers[0]
			if p.HasLimit != tc.known || p.Percent != 0 {
				t.Fatalf("weekly quota replaced primary: %+v", p)
			}
			if tc.known && (p.QuotaResetAt == nil || *p.QuotaResetAt != shortReset.Unix()) {
				t.Fatal("primary reset must be five-hour reset")
			}
			if !tc.known && p.QuotaResetAt != nil {
				t.Fatal("weekly reset must not replace primary countdown")
			}
			if tc.weekly {
				if p.WindowLabel != "5 小時窗" || len(p.OtherLimits) != 1 || p.OtherLimits[0].Label != "7 天" || p.OtherLimits[0].Percent != .4 {
					t.Fatalf("weekly detail missing: %+v", p)
				}
			} else if len(p.OtherLimits) != 0 {
				t.Fatal("expired details retained")
			}
			if len(r.Windows) != 1 && *r.PercentUsed != 0 {
				t.Fatal("stored report mutated")
			}
		})
	}
}

func TestDefaultPolicyStillUsesReportedPrimary(t *testing.T) {
	now := time.Now()
	percent := 40.0
	c := &fakeCollector{batches: []model.Collection{{Reported: &model.Reported{At: now, PercentUsed: &percent,
		Windows: []model.ReportedWindow{{Label: "7 天", WindowMinutes: 10080, PercentUsed: percent, HasPercent: true}}}}}}
	s := testStore(c)
	s.now = func() time.Time { return now }
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if !p.HasLimit || p.Percent != .4 || p.WindowLabel != "7 天窗" || len(p.OtherLimits) != 0 {
		t.Fatalf("default provider behavior changed: %+v", p)
	}
}

func TestDefaultPolicyKeepsWindowsWithPastResets(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	most, other := 30.0, 0.0
	c := &fakeCollector{batches: []model.Collection{{Reported: &model.Reported{At: now, PercentUsed: &most, ResetAt: &past,
		Windows: []model.ReportedWindow{
			{Label: "Gemini / 7 天", WindowMinutes: 10080, PercentUsed: most, HasPercent: true, ResetAt: &past},
			{Label: "Gemini / 5 小時", WindowMinutes: 300, PercentUsed: other, HasPercent: true, ResetAt: &past},
		}}}}}
	s := testStore(c)
	s.now = func() time.Time { return now }
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if !p.HasLimit || p.Percent != .3 || len(p.OtherLimits) != 1 {
		t.Fatalf("provider without expiry policy lost its quota: %+v", p)
	}
}

func TestQuotaIssueSurvivesFallbackAndClearsOnRecovery(t *testing.T) {
	now := time.Now()
	percent := 0.0
	r := &model.Reported{At: now, PercentUsed: &percent}
	c := &fakeCollector{batches: []model.Collection{
		{Reported: r, Stats: model.ScanStats{QuotaIssue: "synthetic login required"}},
		{Reported: r},
	}}
	s := testStore(c)
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if !p.HasLimit || p.QuotaIssue != "synthetic login required" {
		t.Fatal("valid fallback must not hide login problem")
	}
	s.ScanOnce()
	if s.Snapshot().Providers[0].QuotaIssue != "" {
		t.Fatal("recovery retained stale login problem")
	}
}

func TestReportedRemoteSourceCanBeShownWithoutHostInstall(t *testing.T) {
	now := time.Now()
	percent := 12.0
	c := &fakeCollector{policy: model.ProviderPolicy{ShowWhenReported: true}, batches: []model.Collection{{Reported: &model.Reported{At: now, PercentUsed: &percent}}}}
	s := New(testConfig(), nil, map[string]Collector{"test": c}, func() map[string]bool { return map[string]bool{} })
	s.now = func() time.Time { return now }
	s.ScanOnce()
	if len(s.Snapshot().Providers) != 1 {
		t.Fatal("remote source hidden by host installation filter")
	}
	s.now = func() time.Time { return now.Add(25 * time.Hour) }
	if len(s.Snapshot().Providers) != 0 {
		t.Fatal("expired report bypassed installation filter")
	}
}
