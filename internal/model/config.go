package model

import "time"

const (
	ProviderClaude      = "claude"
	ProviderCodex       = "codex"
	ProviderGemini      = "gemini"
	ProviderCursor      = "cursor"
	ProviderAntigravity = "antigravity"
)

// ManualUsage 供沒有本機紀錄可讀的來源（目前是 Cursor）手動填寫。
type ManualUsage struct {
	Used  float64   `json:"used"`
	Limit float64   `json:"limit"`
	AsOf  time.Time `json:"as_of"`
}

type ProviderConfig struct {
	ClaudeOrg string   `json:"claude_org,omitempty"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Roots     []string `json:"roots"`

	Metric        string  `json:"metric"`         // tokens | requests
	WindowKind    string  `json:"window_kind"`    // rolling | day | week | month
	WindowSeconds int64   `json:"window_seconds"` // rolling 專用
	Limit         float64 `json:"limit"`          // 0 = 尚未設定

	Manual *ManualUsage `json:"manual,omitempty"`
	Note   string       `json:"note,omitempty"`
}

type Config struct {
	Port           int              `json:"port"`
	RefreshSeconds int              `json:"refresh_seconds"`
	WarnRatio      float64          `json:"warn_ratio"`
	PreferReported bool             `json:"prefer_reported_limits"`
	RetentionDays  int              `json:"retention_days"`
	Providers      []ProviderConfig `json:"providers"`
}

func (c *Config) Provider(id string) *ProviderConfig {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

// Clone returns an independent configuration safe for editing by another layer.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	copy := *c
	copy.Providers = append([]ProviderConfig(nil), c.Providers...)
	for i := range copy.Providers {
		copy.Providers[i].Roots = append([]string(nil), c.Providers[i].Roots...)
		if c.Providers[i].Manual != nil {
			manual := *c.Providers[i].Manual
			copy.Providers[i].Manual = &manual
		}
	}
	return &copy
}
