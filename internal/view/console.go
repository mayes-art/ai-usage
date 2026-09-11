package view

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"aiusage/internal/model"
)

func Report(w io.Writer, snap model.Snapshot) {

	fmt.Fprintf(w, "AI 用量  %s  (%s)\n\n", time.Unix(snap.At, 0).Format("2006-01-02 15:04:05"), snap.Host)
	for _, p := range snap.Providers {
		// 跟面板一致：百分比在最前面，絕對值退到後面當佐證。
		pct := "   —"
		if p.HasLimit {
			pct = fmt.Sprintf("%3.0f%%", p.Percent*100)
		}
		amount := formatAmount(p.Used, p.Metric)
		switch {
		case p.ShowLimit:
			amount += " / " + formatAmount(p.Limit, p.Metric)
		case p.HasLimit:
			amount += "（上限未公開）"
		default:
			amount += "（未設上限）"
		}
		fmt.Fprintf(w, "%-12s %s %s  %s\n", p.Name, pct, textBar(p.Percent, p.HasLimit), amount)
		detail := "  " + p.WindowLabel
		if p.ResetAt != nil {
			t := time.Unix(*p.ResetAt, 0)
			layout := "15:04"
			if time.Until(t) > 20*time.Hour {
				layout = "01/02 15:04"
			}
			detail += fmt.Sprintf("，%s於 %s", p.ResetLabel, t.Format(layout))
		}
		if p.RatePerMin > 0 {
			detail += fmt.Sprintf("，速率 %s/小時", formatAmount(p.RatePerMin*60, p.Metric))
		}
		// 還有另一道限制時一併講出來，例如 Codex 的 7 天窗。
		for _, w := range p.OtherLimits {
			label := w.Label
			if label == "" {
				label = "另一道"
			}
			detail += fmt.Sprintf("，%s限額 %.0f%%", label, w.Percent*100)
		}
		if p.Status != "ok" && p.Detail != "" {
			detail += "，" + p.Detail
		}
		fmt.Fprintln(w, detail)
		if len(p.Models) > 0 {
			var parts []string
			for _, m := range p.Models {
				v := float64(m.Tokens)
				if p.Metric == "requests" {
					v = float64(m.Requests)
				}
				parts = append(parts, fmt.Sprintf("%s %s", m.Model, formatAmount(v, p.Metric)))
			}
			fmt.Fprintln(w, "  "+strings.Join(parts, "   "))
		}
		fmt.Fprintln(w)
	}
}

func textBar(pct float64, hasLimit bool) string {
	const w = 24
	if !hasLimit {
		return "[" + strings.Repeat("·", w) + "]"
	}
	n := int(pct * w)
	if n > w {
		n = w
	}
	if n < 0 {
		n = 0
	}
	return "[" + strings.Repeat("#", n) + strings.Repeat("·", w-n) + "]"
}

func formatAmount(v float64, metric string) string {
	if metric == "usd" {
		return fmt.Sprintf("US$%.2f", v)
	}
	if metric == "requests" {
		return fmt.Sprintf("%.0f 次", v)
	}
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}

func Doctor(w io.Writer, diagnostics []model.Diagnosis, version, configPath string) {
	fmt.Fprintf(w, "aiusage %s  診斷報告\n", version)
	fmt.Fprintln(w, "設定檔：", configPath)
	fmt.Fprintln(w)

	for _, d := range diagnostics {
		if d.Provider == model.ProviderClaude {
			if d.CLIPath == "" {
				fmt.Fprintln(w, "Claude CLI：未找到；仍可讀取 Desktop 額度快照")
			} else {
				fmt.Fprintln(w, "Claude CLI（含 Desktop 內附版本）：", d.CLIPath)
			}
			if d.Sample != nil && d.Sample.QuotaAt != nil {
				fmt.Fprintf(w, "Claude 額度來源：%s，更新於 %s\n", d.Sample.QuotaSource, time.Unix(*d.Sample.QuotaAt, 0).Local().Format("2006-01-02 15:04:05"))
			}
		}
		fmt.Fprintln(w, "──", d.Provider)
		for _, r := range d.Roots {
			mark := "  找不到"
			for _, h := range d.RootsHit {
				if h == r {
					mark = "  存在"
				}
			}
			fmt.Fprintf(w, "   路徑 %s%s\n", r, mark)
		}
		fmt.Fprintf(w, "   檔案 %d，讀取 %d 行，取得 %d 筆用量事件\n", d.Files, d.Lines, d.Events)
		if len(d.TokenKeys) > 0 {
			fmt.Fprintln(w, "   命中的 token 欄位：")
			for _, k := range d.TokenKeys {
				fmt.Fprintln(w, "     ", k)
			}
		} else if d.Files > 0 {
			fmt.Fprintln(w, "   沒有命中任何 token 欄位（欄位名稱可能與字典不符，請把這段輸出回報）")
		}
		if len(d.QuotaKeys) > 0 {
			fmt.Fprintln(w, "   疑似額度／限制欄位：")
			keys := make([]string, 0, len(d.QuotaKeys))
			for k := range d.QuotaKeys {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(w, "      %s (%v)\n", k, d.QuotaKeys[k])
			}
		} else {
			fmt.Fprintln(w, "   沒有找到額度相關欄位，這一家只能手動設定上限")
		}
		for _, e := range d.Errors {
			fmt.Fprintln(w, "   問題：", e)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "以上只列出欄位名稱與統計，不含任何對話內容。")
}
