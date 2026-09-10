package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// Store 把所有狀態放在記憶體裡。
// 這個工具不需要資料庫：紀錄檔本身就是持久層，重啟時重掃一次即可重建。
type Store struct {
	mu sync.RWMutex
	// scanMu 序列化掃描本身。面板的「重新掃描」與定時掃描可能同時發生，
	// 沒有這道鎖時 Rescan 會在掃描中途把 files 清空，讓 scanProvider 寫進 nil。
	scanMu sync.Mutex
	cfg    *Config

	events   map[string][]Event
	seen     map[string]struct{}
	cum      map[string]Usage // provider|session -> 上次看到的累計值
	reported map[string]*Reported
	files    map[string]*fileState
	stats    map[string]*scanStats
	lastScan map[string]time.Time
	keyPaths map[string]map[string]int

	collectKeys bool
	host        string
	scanDur     time.Duration
}

func NewStore(cfg *Config) *Store {
	host, _ := os.Hostname()
	return &Store{
		cfg:      cfg,
		events:   map[string][]Event{},
		seen:     map[string]struct{}{},
		cum:      map[string]Usage{},
		reported: map[string]*Reported{},
		files:    map[string]*fileState{},
		stats:    map[string]*scanStats{},
		lastScan: map[string]time.Time{},
		keyPaths: map[string]map[string]int{},
		host:     host,
	}
}

func (s *Store) Config() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) UpdateConfig(fn func(*Config)) error {
	s.mu.Lock()
	fn(s.cfg)
	cfg := *s.cfg
	s.mu.Unlock()
	return SaveConfig(&cfg)
}

// Rescan 清掉檔案位移與事件，下次掃描會整份重讀。
func (s *Store) Rescan() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = map[string][]Event{}
	s.seen = map[string]struct{}{}
	s.cum = map[string]Usage{}
	s.files = map[string]*fileState{}
	s.reported = map[string]*Reported{}
	s.keyPaths = map[string]map[string]int{}
}

// ScanOnce 掃描所有啟用的來源一次。
func (s *Store) ScanOnce() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := time.Now()
	cfg := s.Config()
	cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays)

	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if !p.Enabled {
			continue
		}
		s.scanProvider(p, cutoff)
	}
	s.mu.Lock()
	s.scanDur = time.Since(start)
	s.prune(cutoff)
	s.mu.Unlock()
}

func (s *Store) scanProvider(p ProviderConfig, cutoff time.Time) {
	files, hitRoots, errs := discover(p.Roots)
	st := &scanStats{Files: len(files), Errors: errs, RootsHit: hitRoots}

	for _, path := range files {
		s.mu.Lock()
		fstate := s.files[path]
		if fstate == nil {
			fstate = &fileState{}
			s.files[path] = fstate
		}
		local := *fstate
		s.mu.Unlock()

		records, mtime, err := readNew(path, &local, cutoff)
		if err != nil {
			if len(st.Errors) < 5 {
				st.Errors = append(st.Errors, shortPath(path)+": "+err.Error())
			}
			continue
		}

		s.mu.Lock()
		// 這一段期間 files 有可能被換成新的 map，重新取一次而不是直接解參考。
		if cur := s.files[path]; cur != nil {
			*cur = local
		} else {
			saved := local
			s.files[path] = &saved
		}
		for _, raw := range records {
			st.Lines++
			ev, rep, keys := ParseRecord(p.ID, raw, mtime, s.collectKeys)
			if keys != nil {
				m := s.keyPaths[p.ID]
				if m == nil {
					m = map[string]int{}
					s.keyPaths[p.ID] = m
				}
				for k, c := range keys {
					if len(m) < 600 {
						m[k] += c
					}
				}
			}
			if rep != nil && rep.usable() {
				if cur := s.reported[p.ID]; cur == nil || !rep.At.Before(cur.At) {
					s.reported[p.ID] = rep
				}
			}
			if ev == nil {
				continue
			}
			if _, dup := s.seen[ev.Key]; dup {
				st.Skipped++
				continue
			}
			s.seen[ev.Key] = struct{}{}

			if ev.Cumulative {
				// 累計型紀錄：只採計相對上次的增量。
				ck := p.ID + "|" + ev.Session
				prev := s.cum[ck]
				delta := deltaUsage(prev, ev.Usage)
				s.cum[ck] = maxUsage(prev, ev.Usage)
				if delta.empty() {
					continue
				}
				ev.Usage = delta
			}
			if ev.At.Before(cutoff) {
				continue
			}
			s.events[p.ID] = append(s.events[p.ID], *ev)
			st.Events++
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.stats[p.ID] = st
	s.lastScan[p.ID] = time.Now()
	s.mu.Unlock()
}

func deltaUsage(prev, cur Usage) Usage {
	d := func(a, b int64) int64 {
		if b > a {
			return b - a
		}
		return 0
	}
	return Usage{
		Input:      d(prev.Input, cur.Input),
		Output:     d(prev.Output, cur.Output),
		CacheWrite: d(prev.CacheWrite, cur.CacheWrite),
		CacheRead:  d(prev.CacheRead, cur.CacheRead),
		Reasoning:  d(prev.Reasoning, cur.Reasoning),
		Total:      d(prev.Total, cur.Total),
	}
}

func maxUsage(a, b Usage) Usage {
	m := func(x, y int64) int64 {
		if y > x {
			return y
		}
		return x
	}
	return Usage{
		Input:      m(a.Input, b.Input),
		Output:     m(a.Output, b.Output),
		CacheWrite: m(a.CacheWrite, b.CacheWrite),
		CacheRead:  m(a.CacheRead, b.CacheRead),
		Reasoning:  m(a.Reasoning, b.Reasoning),
		Total:      m(a.Total, b.Total),
	}
}

// prune 丟掉保留期外的事件，讓記憶體用量維持穩定。
func (s *Store) prune(cutoff time.Time) {
	for id, evs := range s.events {
		if len(evs) == 0 {
			continue
		}
		keep := evs[:0]
		for _, e := range evs {
			if !e.At.Before(cutoff) {
				keep = append(keep, e)
			}
		}
		s.events[id] = keep
	}
	if len(s.seen) > 400000 {
		s.seen = map[string]struct{}{}
	}
}

// ---------------------------------------------------------------------------
// 面板快照
// ---------------------------------------------------------------------------

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

type ProviderSnapshot struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // ok | idle | no_data | unavailable | manual | disabled
	Detail string `json:"detail"`

	Metric      string `json:"metric"`
	WindowLabel string `json:"window_label"`
	ResetLabel  string `json:"reset_label"`
	ResetAt     *int64 `json:"reset_at,omitempty"`

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

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	snap := Snapshot{
		At:        now.Unix(),
		Host:      s.host,
		ScanMS:    s.scanDur.Milliseconds(),
		WarnRatio: s.cfg.WarnRatio,
	}
	for _, p := range s.cfg.Providers {
		snap.Providers = append(snap.Providers, s.providerSnapshot(p, now))
	}
	return snap
}

func (s *Store) providerSnapshot(p ProviderConfig, now time.Time) ProviderSnapshot {
	ps := ProviderSnapshot{
		ID: p.ID, Name: p.Name, Metric: p.Metric,
		Limit: p.Limit, LimitSource: "unset",
		Note: p.Note, WarnRatio: s.cfg.WarnRatio,
	}
	for _, r := range p.Roots {
		ps.Roots = append(ps.Roots, expandVars(r))
	}
	if p.Limit > 0 {
		ps.LimitSource = "manual"
	}
	if !p.Enabled {
		ps.Status = "disabled"
		ps.Detail = "已在設定中停用"
		return ps
	}

	winStart, winEnd, winLabel := windowBounds(p, now)
	ps.WindowLabel = winLabel

	evs := s.events[p.ID]
	var oldestInWindow time.Time
	var recent15 int64
	var recentReq15, todayReq int
	var todayTokens int64
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	modelAgg := map[string]*ModelUse{}
	var lastAt time.Time

	for _, e := range evs {
		if e.At.After(lastAt) {
			lastAt = e.At
		}
		if !e.At.Before(dayStart) {
			todayTokens += e.Usage.Billable()
			todayReq++
		}
		// 今日以外的事件仍可能落在滾動視窗內，所以不能在這裡 continue。
		if e.At.Before(winStart) || e.At.After(now.Add(time.Minute)) {
			continue
		}
		ps.Tokens.add(e.Usage)
		ps.Requests++
		if e.ApproxTime {
			ps.ApproxTime = true
		}
		if oldestInWindow.IsZero() || e.At.Before(oldestInWindow) {
			oldestInWindow = e.At
		}
		if now.Sub(e.At) <= 15*time.Minute {
			recent15 += e.Usage.Billable()
			recentReq15++
		}
		name := e.Model
		if name == "" {
			name = "（未標示模型）"
		}
		m := modelAgg[name]
		if m == nil {
			m = &ModelUse{Model: name}
			modelAgg[name] = m
		}
		m.Tokens += e.Usage.Billable()
		m.Requests++
	}

	// 百分比的分子
	if p.Metric == "requests" {
		ps.Used = float64(ps.Requests)
		ps.Today = int64(todayReq)
		ps.RatePerMin = float64(recentReq15) / 15
	} else {
		ps.Used = float64(ps.Tokens.Billable())
		ps.Today = todayTokens
		ps.RatePerMin = float64(recent15) / 15
	}

	// 手動填寫（Cursor 這類沒有本機紀錄的來源）
	if p.Manual != nil && p.Manual.Limit > 0 {
		ps.Used = p.Manual.Used
		ps.Limit = p.Manual.Limit
		ps.LimitSource = "manual"
		ps.Status = "manual"
		ps.Detail = "手動填寫，更新於 " + p.Manual.AsOf.Local().Format("01/02 15:04")
	}

	// 若紀錄裡真的找到官方回報的額度，優先採用
	reportedPct := false
	if s.cfg.PreferReported {
		if r := s.reported[p.ID]; r != nil && r.usable() && now.Sub(r.At) < 24*time.Hour {
			if r.PercentUsed != nil {
				ps.Percent = *r.PercentUsed / 100
				ps.LimitSource = "reported"
				reportedPct = true
				if r.Used != nil {
					ps.Used = *r.Used
				}
				if r.Limit != nil {
					ps.Limit = *r.Limit
				}
			} else if r.Used != nil && r.Limit != nil {
				ps.Used, ps.Limit = *r.Used, *r.Limit
				ps.LimitSource = "reported"
			}
			if r.ResetAt != nil {
				winEnd = *r.ResetAt
			}
			if len(r.Windows) > 0 {
				// 進度條畫最短的那個窗，其餘的原樣帶到細節列。
				if l := r.Windows[0].Label; l != "" {
					ps.WindowLabel = l + "窗"
				}
				for _, w := range r.Windows[1:] {
					lw := LimitWindow{Label: w.Label, Percent: w.PercentUsed / 100}
					if w.ResetAt != nil {
						t := w.ResetAt.Unix()
						lw.ResetAt = &t
					}
					ps.OtherLimits = append(ps.OtherLimits, lw)
				}
			}
		}
	}

	// 百分比只有兩個來源：官方回報值，或 用量 / 上限 自己算。
	// 回報值優先，因為它才知道快取、折扣這些我們看不到的計費規則。
	if !reportedPct && ps.Limit > 0 {
		ps.Percent = ps.Used / ps.Limit
	}
	if ps.Percent < 0 || math.IsNaN(ps.Percent) || math.IsInf(ps.Percent, 0) {
		ps.Percent = 0
	}
	// 分母可能只有百分比沒有絕對值（官方只回報 % 的情況），
	// 因此「算得出百分比」與「印得出 used / limit」是兩件事。
	ps.HasLimit = ps.Limit > 0 || reportedPct
	ps.ShowLimit = ps.Limit > 0
	if !ps.HasLimit {
		ps.Percent = 0
	}

	// 重置／滑出時間。
	// rolling 來源的 winEnd 本來是零值，只有在官方回報了重置時間時才會有值；
	// 那種情況要用官方的，不要拿我們自己推的「最舊紀錄滑出」蓋過去。
	switch {
	case !winEnd.IsZero():
		t := winEnd.Unix()
		ps.ResetAt = &t
		ps.ResetLabel = "重置"
	case p.WindowKind == "rolling" && !oldestInWindow.IsZero():
		t := oldestInWindow.Add(time.Duration(p.WindowSeconds) * time.Second).Unix()
		ps.ResetAt = &t
		ps.ResetLabel = "最舊紀錄滑出"
	}

	// 預估耗盡
	if ps.Limit > 0 && ps.RatePerMin > 0 && ps.Used < ps.Limit {
		secs := int64((ps.Limit - ps.Used) / ps.RatePerMin * 60)
		if secs > 0 && secs < 60*60*24*30 {
			ps.ExhaustIn = &secs
		}
	}

	for _, m := range modelAgg {
		ps.Models = append(ps.Models, *m)
	}
	sort.Slice(ps.Models, func(i, j int) bool {
		if ps.Models[i].Tokens != ps.Models[j].Tokens {
			return ps.Models[i].Tokens > ps.Models[j].Tokens
		}
		return ps.Models[i].Model < ps.Models[j].Model
	})
	if len(ps.Models) > 4 {
		ps.Models = ps.Models[:4]
	}
	if !lastAt.IsZero() {
		t := lastAt.Unix()
		ps.LastEventAt = &t
	}
	if st := s.stats[p.ID]; st != nil {
		ps.Files = st.Files
		ps.Errors = st.Errors
	}

	// 狀態判定：要能區分「沒在用」和「壞了」。
	if ps.Status == "" {
		st := s.stats[p.ID]
		switch {
		case st == nil:
			ps.Status = "no_data"
			ps.Detail = "尚未掃描"
		case len(st.RootsHit) == 0:
			ps.Status = "unavailable"
			ps.Detail = "找不到紀錄資料夾"
		case st.Files == 0:
			ps.Status = "no_data"
			ps.Detail = "資料夾在，但裡面沒有可讀的紀錄檔"
		case len(evs) == 0:
			ps.Status = "no_data"
			ps.Detail = fmt.Sprintf("讀到 %d 個檔案，但沒有命中任何用量欄位。執行 aiusage doctor 可看實際欄位名稱", st.Files)
		case ps.Requests == 0:
			ps.Status = "idle"
			ps.Detail = "這個視窗內沒有用量"
		default:
			ps.Status = "ok"
		}
	}
	return ps
}

// windowBounds 算出目前計量視窗的起訖。
func windowBounds(p ProviderConfig, now time.Time) (start, end time.Time, label string) {
	switch p.WindowKind {
	case "day":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 0, 1)
		return start, end, "今日"
	case "week":
		wd := (int(now.Weekday()) + 6) % 7 // 週一為第一天
		start = time.Date(now.Year(), now.Month(), now.Day()-wd, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 0, 7)
		return start, end, "本週"
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 1, 0)
		return start, end, "本月"
	default:
		d := time.Duration(p.WindowSeconds) * time.Second
		if d <= 0 {
			d = 5 * time.Hour
		}
		start = now.Add(-d)
		return start, time.Time{}, humanDur(d) + "滾動視窗"
	}
}

func humanDur(d time.Duration) string {
	h := int(d.Hours())
	if h >= 24 && h%24 == 0 {
		return fmt.Sprintf("%d 天", h/24)
	}
	if h > 0 {
		return fmt.Sprintf("%d 小時", h)
	}
	return fmt.Sprintf("%d 分", int(d.Minutes()))
}

func shortPath(p string) string {
	if len(p) <= 64 {
		return p
	}
	return "..." + p[len(p)-61:]
}
