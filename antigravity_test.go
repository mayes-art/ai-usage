package main

import (
	"math"
	"testing"
	"time"
)

func TestAntigravityGroupedQuota(t *testing.T) {
	data := []byte(`{"response":{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"g-week","window":"weekly","remainingFraction":0.97,"resetTime":"2026-09-16T12:32:28Z"},{"bucketId":"g-5h","window":"5h","remainingFraction":0.999,"resetTime":"2026-09-11T12:50:24Z"}]},{"displayName":"Claude and GPT","buckets":[{"bucketId":"c-week","window":"weekly","remainingFraction":1},{"bucketId":"c-5h","window":"5h","remainingFraction":1}]}]}}`)
	r, err := parseAntigravityQuota(data, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 4 || math.Abs(*r.PercentUsed-3) > 0.00001 || r.Windows[0].Label != "Gemini Models / 7 天" || r.ResetAt == nil {
		t.Fatalf("grouped quota: %+v", r)
	}
	if _, err := parseAntigravityQuota([]byte(`{"response":{"groups":[{"buckets":[{"window":"5h"}]}]}}`), time.Now()); err == nil {
		t.Fatal("missing quota must not become zero")
	}
}

func TestAntigravityModelFallback(t *testing.T) {
	r, err := parseAntigravityModels([]byte(`{"userStatus":{"cascadeModelConfigData":{"clientModelConfigs":[{"label":"Model A","quotaInfo":{"remainingFraction":0}},{"label":"Model B","quotaInfo":{"remainingFraction":1}},{"label":"Unavailable"}]}}}`), time.Now())
	if err != nil || len(r.Windows) != 2 || *r.PercentUsed != 100 {
		t.Fatalf("model fallback: %+v %v", r, err)
	}
}

func TestAntigravityMigration(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.provider(providerGemini) != nil || cfg.provider(providerAntigravity) == nil {
		t.Fatal("default must replace Gemini with Antigravity")
	}
	cfg.Providers = append(cfg.Providers, ProviderConfig{ID: providerGemini, Limit: 123})
	migrateAntigravity(cfg)
	migrateAntigravity(cfg)
	if cfg.provider(providerGemini) != nil || cfg.provider(providerAntigravity).Limit != 0 || cfg.provider(providerCursor) == nil {
		t.Fatal("migration must remove Gemini without transferring limits or removing other providers")
	}
}
