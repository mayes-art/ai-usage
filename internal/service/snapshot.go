package service

import (
	"aiusage/internal/model"
	"fmt"
	"math"
	"sort"
	"time"
)

func (s *Store) Snapshot() model.Snapshot {
	var installed map[string]bool
	if s.installed != nil {
		installed = s.installed()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	snap := model.Snapshot{
		At:        now.Unix(),
		Host:      s.host,
		ScanMS:    s.scanDur.Milliseconds(),
		WarnRatio: s.cfg.WarnRatio,
	}
	for _, p := range s.cfg.Providers {
		if s.installed != nil && !installed[p.ID] {
			continue
		}
		snap.Providers = append(snap.Providers, s.providerSnapshot(p, now))
	}
	return snap
}

func (s *Store) providerSnapshot(p model.ProviderConfig, now time.Time) model.ProviderSnapshot {
	ps := model.ProviderSnapshot{
		ID: p.ID, Name: p.Name, Metric: p.Metric,
		Limit: p.Limit, LimitSource: "unset",
		Note: p.Note, WarnRatio: s.cfg.WarnRatio,
	}
	policy := s.policy(p.ID)
	if policy.Name != "" {
		ps.Name = policy.Name
	}
	if policy.Note != "" {
		ps.Note = policy.Note
	}
	ps.Roots = append([]string(nil), p.Roots...)
	if d, ok := s.diagnoses[p.ID]; ok && d.Roots != nil {
		ps.Roots = append([]string(nil), d.Roots...)
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
	modelAgg := map[string]*model.ModelUse{}
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
		ps.Tokens.Add(e.Usage)
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
			m = &model.ModelUse{Model: name}
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
		if r := s.reported[p.ID]; r != nil && r.Source != "" {
			ps.QuotaSource = r.Source
			t := r.At.Unix()
			ps.QuotaAt = &t
			ps.QuotaStale = now.Sub(r.At) > 45*time.Minute
		}
		if r := s.reported[p.ID]; r != nil && r.Usable() && now.Sub(r.At) < 24*time.Hour {
			if r.Metric != "" {
				ps.Metric = r.Metric
			}
			if policy.AccountAmounts {
				ps.Used, ps.Limit = 0, 0
				ps.RatePerMin = 0
				ps.Today = 0
				winEnd = time.Time{}
			}
			if r.PercentUsed != nil {
				ps.Percent = *r.PercentUsed / 100
				ps.LimitSource = "reported"
				reportedPct = true
				ps.Status = "ok"
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
				t := r.ResetAt.Unix()
				ps.QuotaResetAt = &t
			}
			for _, group := range r.Groups {
				g := model.UsageGroup{ID: group.ID, Label: group.Label}
				if group.PercentUsed != nil {
					percent := *group.PercentUsed / 100
					g.Percent = &percent
					ps.Status = "ok"
					ps.LimitSource = "reported"
				}
				if group.Used != nil {
					used := *group.Used
					g.Used = &used
				}
				if group.Limit != nil {
					limit := *group.Limit
					g.Limit = &limit
				}
				ps.UsageGroups = append(ps.UsageGroups, g)
			}
			if len(r.Windows) > 0 {
				// 進度條畫最短的那個窗，其餘的原樣帶到細節列。
				if l := r.Windows[0].Label; l != "" {
					ps.WindowLabel = l + "窗"
				}
				if r.Windows[0].ResetAt != nil {
					winEnd = *r.Windows[0].ResetAt
					t := r.Windows[0].ResetAt.Unix()
					ps.QuotaResetAt = &t
				}
				for _, w := range r.Windows[1:] {
					lw := model.LimitWindow{Label: w.Label, Percent: w.PercentUsed / 100}
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
		ps.Errors = append([]string(nil), st.Errors...)
		if policy.IncludeDiscoveredRoots {
			ps.Roots = append(ps.Roots, st.RootsHit...)
		}
	}

	// 狀態判定：要能區分「沒在用」和「壞了」。
	if ps.Status == "" {
		st := s.stats[p.ID]
		switch {
		case reportedPct:
			ps.Status = "ok"
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
	if policy.UnavailableDetail != "" && (ps.Status == "unavailable" || ps.Status == "no_data") {
		ps.Detail = policy.UnavailableDetail
	}
	if policy.QuotaOnly {
		ps.Metric = "quota"
		ps.Used = 0
		ps.Limit = 0
		ps.ShowLimit = false
		if !reportedPct {
			ps.HasLimit = false
			ps.Percent = 0
			ps.Status = "unavailable"
			ps.Detail = policy.UnavailableDetail
		}
	}
	return ps
}

// windowBounds 算出目前計量視窗的起訖。
func windowBounds(p model.ProviderConfig, now time.Time) (start, end time.Time, label string) {
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

func (s *Store) policy(id string) model.ProviderPolicy {
	if source, ok := s.collectors[id].(interface{ Policy() model.ProviderPolicy }); ok {
		return source.Policy()
	}
	return model.ProviderPolicy{}
}
