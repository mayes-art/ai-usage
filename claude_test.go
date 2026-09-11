package main

import (
	"testing"
	"time"
)

func TestClaudeDesktopHistory(t *testing.T) {
	now := time.UnixMilli(1789107732284)
	data := []byte(`{"version":2,"samples":[{"t":1789107732284,"org":"new","u":{"fh":0,"sd":33}},{"t":1789106832196,"org":"old","u":{"fh":75,"sd":17}}]}`)
	r, err := parseClaudeHistory(data, "", now)
	if err != nil || r == nil || *r.PercentUsed != 0 || len(r.Windows) != 2 || r.Windows[1].PercentUsed != 33 || r.ResetAt != nil {
		t.Fatalf("latest zero-valued sample must be used without invented resets: %+v %v", r, err)
	}
	r, err = parseClaudeHistory(data, "old", now)
	if err != nil || *r.PercentUsed != 75 {
		t.Fatalf("organization filter: %+v %v", r, err)
	}
	_, err = parseClaudeHistory([]byte(`{"version":2,"samples":[{"t":1789107732284,"u":{}},{"t":1789106832196,"u":{"fh":50}}]}`), "", now)
	if err == nil {
		t.Fatal("must not substitute older sample when newest has no quota")
	}
	r, err = parseClaudeHistory([]byte(`{"version":1,"samples":[{"t":1789107732284,"fh":null,"sd":9}]}`), "", now)
	if err != nil || len(r.Windows) != 1 || r.Windows[0].WindowMinutes != 10080 {
		t.Fatalf("v1: %+v %v", r, err)
	}
}

func TestClaudeQuotaNotContext(t *testing.T) {
	_, r, _ := ParseRecord(providerClaude, []byte(`{"context_window":{"used_percentage":81}}`), time.Now(), false)
	if r != nil {
		t.Fatal("context percentage is not plan quota")
	}
	_, r, _ = ParseRecord(providerClaude, []byte(`{"context_window":{"used_percentage":81},"rate_limits":{"five_hour":{"used_percentage":0,"resets_at":1789107732},"seven_day":{"used_percentage":33}}}`), time.Now(), false)
	if r == nil || *r.PercentUsed != 0 || len(r.Windows) != 2 || r.ResetAt == nil {
		t.Fatalf("CLI quota: %+v", r)
	}
}

func TestClaudeQuotaWithoutTokenLogs(t *testing.T) {
	cfg := DefaultConfig()
	s := NewStore(cfg)
	now := time.Now()
	zero := 0.0
	s.reported[providerClaude] = &Reported{At: now, PercentUsed: &zero, Source: "Claude Desktop 額度快照"}
	for _, p := range s.Snapshot().Providers {
		if p.ID == providerClaude && (p.Status != "ok" || !p.HasLimit || p.QuotaAt == nil) {
			t.Fatalf("quota-only snapshot: %+v", p)
		}
	}
}
