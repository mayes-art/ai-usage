package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	providerClaude      = "claude"
	providerCodex       = "codex"
	providerGemini      = "gemini"
	providerCursor      = "cursor"
	providerAntigravity = "antigravity"
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

func DefaultConfig() *Config {
	home := userHome()
	return &Config{
		Port:           0, // 0 = 隨機挑一個可用埠
		RefreshSeconds: 3,
		WarnRatio:      0.8,
		PreferReported: true,
		RetentionDays:  40,
		Providers: []ProviderConfig{
			{
				ID: providerClaude, Name: "Claude Code", Enabled: true,
				Roots:      []string{filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".claude", "history")},
				Metric:     "tokens",
				WindowKind: "rolling", WindowSeconds: 5 * 3600,
				Note: "5 小時滾動視窗。上限請依你的方案自行填入，或等工具在紀錄中找到官方回報值。",
			},
			{
				ID: providerCodex, Name: "Codex CLI", Enabled: true,
				Roots:      []string{filepath.Join(home, ".codex", "sessions"), filepath.Join(home, ".codex", "log")},
				Metric:     "tokens",
				WindowKind: "rolling", WindowSeconds: 5 * 3600,
			},
			{
				ID: providerAntigravity, Name: "Antigravity CLI / Desktop", Enabled: true,
				Metric: "quota", WindowKind: "rolling", WindowSeconds: 5 * 3600,
				Note: "讀取執行中 Antigravity 的模型群額度；請保持 CLI 或 Desktop 開啟並登入。",
			},
			{
				ID: providerCursor, Name: "Cursor", Enabled: true,
				Roots:      []string{filepath.Join(home, ".cursor")},
				Metric:     "requests",
				WindowKind: "month",
				Note:       "Cursor 未在本機留下可讀的用量紀錄，請在面板上手動填入後台看到的數字。",
			},
		},
	}
}

func (c *Config) provider(id string) *ProviderConfig {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 路徑
// ---------------------------------------------------------------------------

func userHome() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if p := os.Getenv("USERPROFILE"); p != "" {
			return p
		}
	}
	return os.Getenv("HOME")
}

// ConfigDir 是設定檔位置：
// Windows 用 %APPDATA%\aiusage，其他平台用 ~/.config/aiusage。
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("APPDATA"); p != "" {
			return filepath.Join(p, "aiusage")
		}
		return filepath.Join(userHome(), "AppData", "Roaming", "aiusage")
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "aiusage")
	}
	return filepath.Join(userHome(), ".config", "aiusage")
}

func configPath() string { return filepath.Join(ConfigDir(), "config.json") }

// expandVars 展開 %VAR% 與 $VAR，讓設定檔可以手寫環境變數。
func expandVars(p string) string {
	if strings.ContainsRune(p, '%') {
		parts := strings.Split(p, "%")
		var b strings.Builder
		for i, seg := range parts {
			if i%2 == 1 {
				if v := os.Getenv(seg); v != "" {
					b.WriteString(v)
					continue
				}
				b.WriteString("%" + seg + "%")
				continue
			}
			b.WriteString(seg)
		}
		p = b.String()
	}
	return os.ExpandEnv(p)
}

// LoadConfig 讀設定；不存在就寫一份預設檔出來。
// 已存在的設定會與預設值合併，好讓新版新增的來源自動出現。
func LoadConfig() (*Config, error) {
	def := DefaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			_ = SaveConfig(def)
			return def, nil
		}
		return def, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return def, err
	}
	migrateAntigravity(&cfg)
	if cfg.RefreshSeconds <= 0 {
		cfg.RefreshSeconds = def.RefreshSeconds
	}
	if cfg.WarnRatio <= 0 || cfg.WarnRatio >= 1 {
		cfg.WarnRatio = def.WarnRatio
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = def.RetentionDays
	}
	// 補上設定檔裡沒有的來源。
	for _, dp := range def.Providers {
		if cfg.provider(dp.ID) == nil {
			cfg.Providers = append(cfg.Providers, dp)
		}
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Metric == "" {
			p.Metric = "tokens"
		}
		if p.WindowKind == "" {
			p.WindowKind = "rolling"
		}
		if p.WindowKind == "rolling" && p.WindowSeconds <= 0 {
			p.WindowSeconds = 5 * 3600
		}
		if len(p.Roots) == 0 {
			if d := def.provider(p.ID); d != nil {
				p.Roots = d.Roots
			}
		}
	}
	return &cfg, nil
}

// Remove the old Gemini row without transferring its API-key limits or roots.
func migrateAntigravity(cfg *Config) {
	providers := cfg.Providers[:0]
	for _, p := range cfg.Providers {
		if p.ID != providerGemini {
			providers = append(providers, p)
		}
	}
	cfg.Providers = providers
}

func SaveConfig(c *Config) error {
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}
