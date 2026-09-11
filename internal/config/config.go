package config

import (
	"aiusage/internal/model"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DefaultConfig() *model.Config {
	home := UserHome()
	return &model.Config{
		Port:           0, // 0 = 隨機挑一個可用埠
		RefreshSeconds: 3,
		WarnRatio:      0.8,
		PreferReported: true,
		RetentionDays:  40,
		Providers: []model.ProviderConfig{
			{
				ID: model.ProviderClaude, Name: "Claude Code", Enabled: true,
				Roots:      []string{filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".claude", "history")},
				Metric:     "tokens",
				WindowKind: "rolling", WindowSeconds: 5 * 3600,
				Note: "5 小時滾動視窗。上限請依你的方案自行填入，或等工具在紀錄中找到官方回報值。",
			},
			{
				ID: model.ProviderCodex, Name: "Codex CLI", Enabled: true,
				Roots:      []string{filepath.Join(home, ".codex", "sessions"), filepath.Join(home, ".codex", "log")},
				Metric:     "tokens",
				WindowKind: "rolling", WindowSeconds: 5 * 3600,
			},
			{
				ID: model.ProviderAntigravity, Name: "Antigravity CLI / Desktop", Enabled: true,
				Metric: "quota", WindowKind: "rolling", WindowSeconds: 5 * 3600,
				Note: "讀取執行中 Antigravity 的模型群額度；請保持 CLI 或 Desktop 開啟並登入。",
			},
			{
				ID: model.ProviderCursor, Name: "Cursor", Enabled: true,
				Roots:      []string{filepath.Join(home, ".cursor")},
				Metric:     "requests",
				WindowKind: "month",
				Note:       "Cursor 未在本機留下可讀的用量紀錄，請在面板上手動填入後台看到的數字。",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// 路徑
// ---------------------------------------------------------------------------

func UserHome() string {
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
		return filepath.Join(UserHome(), "AppData", "Roaming", "aiusage")
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "aiusage")
	}
	return filepath.Join(UserHome(), ".config", "aiusage")
}

func ConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }

// ExpandVars 展開 %VAR% 與 $VAR，讓設定檔可以手寫環境變數。
func ExpandVars(p string) string {
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
func LoadConfig() (*model.Config, error) {
	def := DefaultConfig()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			_ = SaveConfig(def)
			return def, nil
		}
		return def, err
	}
	var cfg model.Config
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
		if cfg.Provider(dp.ID) == nil {
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
			if d := def.Provider(p.ID); d != nil {
				p.Roots = d.Roots
			}
		}
	}
	return &cfg, nil
}

// Remove the old Gemini row without transferring its API-key limits or roots.
func migrateAntigravity(cfg *model.Config) {
	providers := cfg.Providers[:0]
	for _, p := range cfg.Providers {
		if p.ID != model.ProviderGemini {
			providers = append(providers, p)
		}
	}
	cfg.Providers = providers
}

func SaveConfig(c *model.Config) error {
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := ConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigPath())
}

// FileRepository persists configuration in the platform-specific config directory.
// It satisfies the configuration repository required by the application service.
type FileRepository struct{}

func (FileRepository) Save(cfg *model.Config) error { return SaveConfig(cfg) }
