package config

import (
	"aiusage/internal/model"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExistingConfigPlacesGeminiBeforeCursor(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, hasGemini := range []bool{false, true} {
		cfg := DefaultConfig()
		gemini := *cfg.Provider(model.ProviderGemini)
		gemini.Enabled = false
		cfg.Providers = []model.ProviderConfig{
			*cfg.Provider(model.ProviderClaude), *cfg.Provider(model.ProviderCodex),
			*cfg.Provider(model.ProviderCursor), *cfg.Provider(model.ProviderAntigravity),
		}
		cfg.Providers[2].Limit = 123
		if hasGemini {
			cfg.Providers = append(cfg.Providers, gemini)
		}
		if err := os.MkdirAll(filepath.Dir(ConfigPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(cfg)
		if err := os.WriteFile(ConfigPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if providerIndex(loaded, model.ProviderGemini)+1 != providerIndex(loaded, model.ProviderCursor) {
			t.Fatalf("Gemini must precede Cursor after upgrade: %+v", loaded.Providers)
		}
		if loaded.Provider(model.ProviderCursor).Limit != 123 || (hasGemini && loaded.Provider(model.ProviderGemini).Enabled) {
			t.Fatal("upgrade changed saved provider settings")
		}
		placeGeminiBeforeCursor(loaded)
		if providerIndex(loaded, model.ProviderGemini)+1 != providerIndex(loaded, model.ProviderCursor) {
			t.Fatal("provider placement is not idempotent")
		}
	}
}

func TestAntigravityMigration(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider(model.ProviderGemini) == nil || cfg.Provider(model.ProviderAntigravity) == nil {
		t.Fatal("default must include Gemini desktop and Antigravity")
	}
	if i, j := providerIndex(cfg, model.ProviderGemini), providerIndex(cfg, model.ProviderCursor); i < 0 || j < 0 || i > j {
		t.Fatal("Gemini must render above Cursor")
	}
	cfg.Providers = append(cfg.Providers, model.ProviderConfig{ID: model.ProviderGemini, Limit: 123, Roots: []string{"legacy"}})
	migrateAntigravity(cfg)
	migrateAntigravity(cfg)
	if cfg.Provider(model.ProviderGemini) == nil || cfg.Provider(model.ProviderGemini).Limit != 0 || len(cfg.Provider(model.ProviderGemini).Roots) != 0 || cfg.Provider(model.ProviderCursor) == nil {
		t.Fatal("migration must drop a legacy Gemini row and keep the desktop provider")
	}
}

func providerIndex(cfg *model.Config, id string) int {
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == id {
			return i
		}
	}
	return -1
}
