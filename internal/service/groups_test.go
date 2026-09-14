package service

import (
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestUsageGroupsFreshnessAndIsolation(t *testing.T) {
	now := time.Now()
	percent, used, limit := 25.0, 5.0, 20.0
	r := &model.Reported{At: now, Source: "synthetic account", Metric: "usd", Groups: []model.ReportedGroup{
		{ID: "first", Label: "First", PercentUsed: &percent, Used: &used, Limit: &limit},
		{ID: "second", Label: "Second"},
	}}
	c := &fakeCollector{batches: []model.Collection{{Reported: r}}, policy: model.ProviderPolicy{AccountAmounts: true}}
	s := testStore(c)
	s.now = func() time.Time { return now }
	s.ScanOnce()
	p := s.Snapshot().Providers[0]
	if p.Status != "ok" || p.LimitSource != "reported" || p.HasLimit || len(p.UsageGroups) != 2 || *p.UsageGroups[0].Percent != .25 || p.UsageGroups[1].Percent != nil {
		t.Fatalf("groups-only report fabricated total or lost pool: %+v", p)
	}
	*p.UsageGroups[0].Percent = .99
	*p.UsageGroups[0].Used = 999
	*p.UsageGroups[0].Limit = 999
	p = s.Snapshot().Providers[0]
	if *p.UsageGroups[0].Percent != .25 || *p.UsageGroups[0].Used != 5 || *p.UsageGroups[0].Limit != 20 {
		t.Fatal("snapshot shares mutable pool data")
	}
	s.now = func() time.Time { return now.Add(time.Hour) }
	if p = s.Snapshot().Providers[0]; !p.QuotaStale || len(p.UsageGroups) != 2 {
		t.Fatal("stale pools must keep warning")
	}
	s.now = func() time.Time { return now.Add(24 * time.Hour) }
	if len(s.Snapshot().Providers[0].UsageGroups) != 0 {
		t.Fatal("expired pools still visible")
	}
	s.now = func() time.Time { return now }
	s.UpdateConfig(func(cfg *model.Config) { cfg.PreferReported = false })
	if len(s.Snapshot().Providers[0].UsageGroups) != 0 {
		t.Fatal("manual preference leaked reported pools")
	}
	s.UpdateConfig(func(cfg *model.Config) { cfg.PreferReported = true; cfg.Providers[0].Enabled = false })
	if len(s.Snapshot().Providers[0].UsageGroups) != 0 {
		t.Fatal("disabled source leaked pools")
	}
}
