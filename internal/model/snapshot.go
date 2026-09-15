package model

type ModelUse struct {
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Requests int    `json:"requests"`
}

// LimitWindow 是「還有另一道限制」。Codex 同時有 5 小時與 7 天兩個窗，
// 進度條只能畫一個，另一個放到細節列，才不會讓人以為只剩一種限制。
type LimitWindow struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
	ResetAt *int64  `json:"reset_at,omitempty"`
}

type UsageGroup struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Percent *float64 `json:"percent,omitempty"` // ratio; nil is unknown, not zero
	Used    *float64 `json:"used,omitempty"`
	Limit   *float64 `json:"limit,omitempty"`
}

type ProviderSnapshot struct {
	QuotaSource string `json:"quota_source,omitempty"`
	QuotaAt     *int64 `json:"quota_at,omitempty"`
	QuotaStale  bool   `json:"quota_stale,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"` // ok | idle | no_data | unavailable | manual | disabled
	Detail      string `json:"detail"`

	Metric      string `json:"metric"`
	WindowLabel string `json:"window_label"`
	ResetLabel  string `json:"reset_label"`
	ResetAt     *int64 `json:"reset_at,omitempty"`
	// QuotaResetAt 只來自帳號回報，本機滾動視窗事件不得寫入。
	QuotaResetAt *int64 `json:"quota_reset_at,omitempty"`

	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	LimitSource string  `json:"limit_source"` // manual | reported | unset
	Percent     float64 `json:"percent"`      // 0..n，1 = 剛好用滿
	// HasLimit 表示這一列算得出百分比。分母可能來自手動上限，
	// 也可能來自紀錄裡的官方回報百分比（此時 Limit 會是 0）。
	HasLimit bool `json:"has_limit"`
	// ShowLimit 表示 used / limit 這組數字有意義，可以印在百分比底下。
	ShowLimit bool `json:"show_limit"`

	Tokens   Usage `json:"tokens"`
	Requests int   `json:"requests"`
	Today    int64 `json:"today"`

	RatePerMin float64 `json:"rate_per_min"`
	ExhaustIn  *int64  `json:"exhaust_in_sec,omitempty"`

	OtherLimits []LimitWindow `json:"other_limits,omitempty"`
	UsageGroups []UsageGroup  `json:"usage_groups,omitempty"`

	Models      []ModelUse `json:"models"`
	LastEventAt *int64     `json:"last_event_at,omitempty"`
	ApproxTime  bool       `json:"approx_time"`
	Files       int        `json:"files"`
	Roots       []string   `json:"roots"`
	Errors      []string   `json:"errors,omitempty"`
	Note        string     `json:"note,omitempty"`
	WarnRatio   float64    `json:"warn_ratio"`
}

type Snapshot struct {
	At        int64              `json:"at"`
	Host      string             `json:"host"`
	ScanMS    int64              `json:"scan_ms"`
	WarnRatio float64            `json:"warn_ratio"`
	Providers []ProviderSnapshot `json:"providers"`
}

type Diagnosis struct {
	CLIPath     string            `json:"cli_path,omitempty"`
	DesktopDirs []string          `json:"desktop_dirs,omitempty"`
	Provider    string            `json:"provider"`
	Roots       []string          `json:"roots"`
	RootsHit    []string          `json:"roots_hit"`
	Files       int               `json:"files"`
	Lines       int               `json:"lines"`
	Events      int               `json:"events"`
	Errors      []string          `json:"errors,omitempty"`
	QuotaKeys   map[string]any    `json:"quota_keys,omitempty"`
	TokenKeys   []string          `json:"token_keys,omitempty"`
	Sample      *ProviderSnapshot `json:"sample,omitempty"`
}

type ScanStats struct {
	Files    int      `json:"files"`
	Lines    int      `json:"lines"`
	Events   int      `json:"events"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
	RootsHit []string `json:"roots_hit,omitempty"`
}

// Collection is the result of collecting a provider's usage at one point in time.
type Collection struct {
	Events        []Event
	Reported      *Reported
	Stats         ScanStats
	Diagnosis     Diagnosis
	ClearReported bool
}

// ProviderPolicy describes presentation behavior without provider-specific logic
// in the application service.
type ProviderPolicy struct {
	Name                   string
	Note                   string
	UnavailableDetail      string
	QuotaOnly              bool
	AccountAmounts         bool
	IncludeDiscoveredRoots bool
}
