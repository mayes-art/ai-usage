package config

import (
	"aiusage/internal/model"
	"testing"
)

func TestAntigravityMigration(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider(model.ProviderGemini) != nil || cfg.Provider(model.ProviderAntigravity) == nil {
		t.Fatal("default must replace Gemini with Antigravity")
	}
	cfg.Providers = append(cfg.Providers, model.ProviderConfig{ID: model.ProviderGemini, Limit: 123})
	migrateAntigravity(cfg)
	migrateAntigravity(cfg)
	if cfg.Provider(model.ProviderGemini) != nil || cfg.Provider(model.ProviderAntigravity).Limit != 0 || cfg.Provider(model.ProviderCursor) == nil {
		t.Fatal("migration must remove Gemini without transferring limits or removing other providers")
	}
}
