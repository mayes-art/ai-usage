package model

import "time"

// Usage 是正規化後的 token 計數。各家 CLI 的欄位名稱不同，
// 這裡收斂成同一組欄位。
type Usage struct {
	Tool       int64 `json:"tool"`
	UseTotal   bool  `json:"-"`
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheWrite int64 `json:"cache_write"`
	CacheRead  int64 `json:"cache_read"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

// Billable 回傳用於進度條的 token 總量。
// 若來源只給 total，就用 total；否則自行加總分項。
func (u Usage) Billable() int64 {
	if u.UseTotal {
		return u.Total
	}
	sum := u.Input + u.Output + u.CacheWrite + u.CacheRead + u.Reasoning
	if sum == 0 {
		return u.Total
	}
	return sum
}

func (u Usage) Empty() bool {
	return u.Input == 0 && u.Output == 0 && u.CacheWrite == 0 &&
		u.CacheRead == 0 && u.Reasoning == 0 && u.Total == 0
}

func (u *Usage) Add(o Usage) {
	u.Tool += o.Tool
	u.UseTotal = u.UseTotal || o.UseTotal
	u.Input += o.Input
	u.Output += o.Output
	u.CacheWrite += o.CacheWrite
	u.CacheRead += o.CacheRead
	u.Reasoning += o.Reasoning
	u.Total += o.Total
}

// Event 是一筆去重後的用量事件。
type Event struct {
	Provider   string    `json:"provider"`
	At         time.Time `json:"at"`
	ApproxTime bool      `json:"approx_time"`
	Model      string    `json:"model"`
	Session    string    `json:"session"`
	Usage      Usage     `json:"usage"`
	Cumulative bool      `json:"cumulative"`
	Key        string    `json:"-"`
}

// Reported 是「來源自己回報的額度資訊」。
// 若某家 CLI 真的把剩餘額度寫進本機紀錄，就用它當進度條分母，
// 不必靠使用者手動填上限。
type Reported struct {
	Metric      string             `json:"metric,omitempty"`
	Source      string             `json:"source,omitempty"`
	At          time.Time          `json:"at"`
	PercentUsed *float64           `json:"percent_used,omitempty"`
	Used        *float64           `json:"used,omitempty"`
	Limit       *float64           `json:"limit,omitempty"`
	ResetAt     *time.Time         `json:"reset_at,omitempty"`
	Windows     []ReportedWindow   `json:"windows,omitempty"`
	Fields      map[string]float64 `json:"fields,omitempty"`
	Labels      map[string]string  `json:"labels,omitempty"`
}

// ReportedWindow 是一個獨立的計量視窗。
// 一家工具可以同時有好幾個：Codex 就同時回報 5 小時窗與 7 天窗，
// 兩組欄位名稱一模一樣，只差在上層是 primary 還是 secondary。
type ReportedWindow struct {
	Label         string     `json:"label"`
	PercentUsed   float64    `json:"percent_used"`
	WindowMinutes float64    `json:"window_minutes,omitempty"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	HasPercent    bool       `json:"-"`
}

func (r Reported) Usable() bool {
	if r.PercentUsed != nil {
		return true
	}
	return r.Used != nil && r.Limit != nil && *r.Limit > 0
}
