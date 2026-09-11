package record

import (
	"aiusage/internal/model"
	"time"
)

func ParseClaudeQuota(node any, at time.Time) *model.Reported {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	limits, ok := m["rate_limits"].(map[string]any)
	if !ok {
		return nil
	}
	r := &model.Reported{At: at, Source: "Claude CLI 額度紀錄"}
	for _, spec := range []struct {
		key  string
		mins float64
	}{{"five_hour", 300}, {"seven_day", 10080}} {
		w, ok := limits[spec.key].(map[string]any)
		if !ok {
			continue
		}
		v, valid := ToNumber(w["used_percentage"])
		if !valid {
			v, valid = ToNumber(w["utilization"])
		}
		if !valid || v < 0 || v > 100 {
			continue
		}
		win := model.ReportedWindow{Label: WindowLabel(spec.mins), WindowMinutes: spec.mins, PercentUsed: v, HasPercent: true}
		if n, ok := ToNumber(w["resets_at"]); ok {
			t, valid := EpochToTime(n)
			if valid {
				win.ResetAt = &t
			}
		}
		if str, ok := w["resets_at"].(string); ok {
			t, valid := ParseTimeString(str)
			if valid {
				win.ResetAt = &t
			}
		}
		r.Windows = append(r.Windows, win)
	}
	if len(r.Windows) == 0 {
		_, ctx := ExtractRecord(limits, false)
		r = BuildReported(ctx, at)
		if r == nil || !r.Usable() {
			return nil
		}
		r.Source = "Claude CLI 額度紀錄"
		return r
	}
	r.PercentUsed, r.ResetAt = &r.Windows[0].PercentUsed, r.Windows[0].ResetAt
	return r
}
