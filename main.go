package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const version = "0.1.0"

func main() {
	cmd := ""
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	port := flag.Int("port", -1, "面板使用的本機埠，0 表示自動挑選")
	interval := flag.Int("interval", 0, "掃描間隔秒數")
	noBrowser := flag.Bool("no-browser", false, "啟動後不要自動開啟瀏覽器")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "設定檔讀取有問題，改用預設值:", err)
	}
	if *port >= 0 {
		cfg.Port = *port
	}
	if *interval > 0 {
		cfg.RefreshSeconds = *interval
	}

	switch cmd {
	case "", "panel", "serve":
		runPanel(cfg, *noBrowser)
	case "report":
		runReport(cfg)
	case "doctor":
		runDoctor(cfg)
	case "config":
		fmt.Println(configPath())
	case "version":
		fmt.Println("aiusage", version, runtime.GOOS+"/"+runtime.GOARCH)
	default:
		fmt.Fprintf(os.Stderr, "未知的指令 %q。可用：panel、report、doctor、config、version\n", cmd)
		os.Exit(2)
	}
}

func runPanel(cfg *Config, noBrowser bool) {
	// 已經有一個實例在跑就直接把面板叫出來，不要開第二個。
	if url, ok := existingInstance(); ok {
		logf("面板已在執行中，直接開啟：%s", url)
		if !noBrowser {
			openBrowser(url)
		}
		return
	}

	store := NewStore(cfg)
	logf("正在讀取紀錄…")
	store.ScanOnce()

	srv := NewServer(store)
	url, closeFn, err := srv.Serve(cfg.Port)
	if err != nil {
		logf("無法啟動面板：%v", err)
		os.Exit(1)
	}
	defer closeFn()
	writeInstanceURL(url)
	defer clearInstanceURL()

	logf("面板已啟動：%s", url)
	logf("設定檔：%s", configPath())
	logf("按 Ctrl+C 結束。")
	if !noBrowser {
		openBrowser(url)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	tick := time.NewTicker(time.Duration(cfg.RefreshSeconds) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			store.ScanOnce()
		case <-stop:
			logf("已結束。")
			return
		}
	}
}

func runReport(cfg *Config) {
	store := NewStore(cfg)
	store.ScanOnce()
	snap := store.Snapshot()

	fmt.Printf("AI 用量  %s  (%s)\n\n", time.Unix(snap.At, 0).Format("2006-01-02 15:04:05"), snap.Host)
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
		fmt.Printf("%-12s %s %s  %s\n", p.Name, pct, textBar(p.Percent, p.HasLimit), amount)
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
		fmt.Println(detail)
		if len(p.Models) > 0 {
			var parts []string
			for _, m := range p.Models {
				v := float64(m.Tokens)
				if p.Metric == "requests" {
					v = float64(m.Requests)
				}
				parts = append(parts, fmt.Sprintf("%s %s", m.Model, formatAmount(v, p.Metric)))
			}
			fmt.Println("  " + strings.Join(parts, "   "))
		}
		fmt.Println()
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

// ---------------------------------------------------------------------------
// doctor：確認各家紀錄裡到底有什麼
// ---------------------------------------------------------------------------

type Diagnosis struct {
	Provider  string            `json:"provider"`
	Roots     []string          `json:"roots"`
	RootsHit  []string          `json:"roots_hit"`
	Files     int               `json:"files"`
	Lines     int               `json:"lines"`
	Events    int               `json:"events"`
	Errors    []string          `json:"errors,omitempty"`
	QuotaKeys map[string]any    `json:"quota_keys,omitempty"`
	TokenKeys []string          `json:"token_keys,omitempty"`
	Sample    *ProviderSnapshot `json:"sample,omitempty"`
}

func (s *Store) Diagnose() []Diagnosis {
	s.mu.RLock()
	cfg := s.cfg
	var out []Diagnosis
	snap := map[string]ProviderSnapshot{}
	s.mu.RUnlock()

	for _, p := range s.Snapshot().Providers {
		snap[p.ID] = p
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range cfg.Providers {
		d := Diagnosis{Provider: p.ID}
		for _, r := range p.Roots {
			d.Roots = append(d.Roots, expandVars(r))
		}
		if st := s.stats[p.ID]; st != nil {
			d.Files, d.Lines, d.Events, d.Errors, d.RootsHit = st.Files, st.Lines, st.Events, st.Errors, st.RootsHit
		}
		// 只列欄位名稱，不列內容。
		var tokenKeys []string
		quota := map[string]any{}
		for path := range s.keyPaths[p.ID] {
			if matchTokenField(lastSeg(path)) != nil {
				tokenKeys = append(tokenKeys, path)
			}
			if looksQuota(lastSeg(path)) {
				quota[path] = s.keyPaths[p.ID][path]
			}
		}
		sort.Strings(tokenKeys)
		if len(tokenKeys) > 30 {
			tokenKeys = tokenKeys[:30]
		}
		d.TokenKeys = tokenKeys
		if len(quota) > 0 {
			d.QuotaKeys = quota
		}
		if r := s.reported[p.ID]; r != nil {
			if d.QuotaKeys == nil {
				d.QuotaKeys = map[string]any{}
			}
			d.QuotaKeys["_reported_usable"] = r.usable()
		}
		if v, ok := snap[p.ID]; ok {
			vv := v
			d.Sample = &vv
		}
		out = append(out, d)
	}
	return out
}

func runDoctor(cfg *Config) {
	store := NewStore(cfg)
	store.collectKeys = true
	store.ScanOnce()

	fmt.Printf("aiusage %s  診斷報告\n", version)
	fmt.Println("設定檔：", configPath())
	fmt.Println()

	for _, d := range store.Diagnose() {
		fmt.Println("──", d.Provider)
		for _, r := range d.Roots {
			mark := "  找不到"
			for _, h := range d.RootsHit {
				if h == r {
					mark = "  存在"
				}
			}
			fmt.Printf("   路徑 %s%s\n", r, mark)
		}
		fmt.Printf("   檔案 %d，讀取 %d 行，取得 %d 筆用量事件\n", d.Files, d.Lines, d.Events)
		if len(d.TokenKeys) > 0 {
			fmt.Println("   命中的 token 欄位：")
			for _, k := range d.TokenKeys {
				fmt.Println("     ", k)
			}
		} else if d.Files > 0 {
			fmt.Println("   沒有命中任何 token 欄位（欄位名稱可能與字典不符，請把這段輸出回報）")
		}
		if len(d.QuotaKeys) > 0 {
			fmt.Println("   疑似額度／限制欄位：")
			keys := make([]string, 0, len(d.QuotaKeys))
			for k := range d.QuotaKeys {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("      %s (%v)\n", k, d.QuotaKeys[k])
			}
		} else {
			fmt.Println("   沒有找到額度相關欄位，這一家只能手動設定上限")
		}
		for _, e := range d.Errors {
			fmt.Println("   問題：", e)
		}
		fmt.Println()
	}
	fmt.Println("以上只列出欄位名稱與統計，不含任何對話內容。")
}

// openBrowser 在 Windows 上優先用 Edge 的 app 模式開一個沒有網址列的視窗，
// 這樣看起來就是個獨立小工具，而不是一個瀏覽器分頁。
func openBrowser(url string) {
	if runtime.GOOS == "windows" {
		for _, exe := range appModeBrowsers() {
			if _, err := os.Stat(exe); err != nil {
				continue
			}
			cmd := exec.Command(exe, "--app="+url, "--window-size=720,760")
			if err := cmd.Start(); err == nil {
				return
			}
		}
		if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err == nil {
			return
		}
		logf("（無法自動開啟視窗，請手動貼上網址）")
		return
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", url)
	} else {
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		logf("（無法自動開啟瀏覽器，請手動貼上網址）")
	}
}

func appModeBrowsers() []string {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LOCALAPPDATA")
	return []string{
		pf86 + `\Microsoft\Edge\Application\msedge.exe`,
		pf + `\Microsoft\Edge\Application\msedge.exe`,
		pf + `\Google\Chrome\Application\chrome.exe`,
		pf86 + `\Google\Chrome\Application\chrome.exe`,
		local + `\Google\Chrome\Application\chrome.exe`,
	}
}

func instanceFile() string { return filepath.Join(ConfigDir(), "panel.url") }

// existingInstance 檢查上一個實例是否還活著。
func existingInstance() (string, bool) {
	data, err := os.ReadFile(instanceFile())
	if err != nil {
		return "", false
	}
	url := strings.TrimSpace(string(data))
	if url == "" {
		return "", false
	}
	check := strings.Replace(url, "/?t=", "/api/snapshot?t=", 1)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(check)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	return url, true
}

func writeInstanceURL(url string) {
	_ = os.MkdirAll(ConfigDir(), 0o700)
	_ = os.WriteFile(instanceFile(), []byte(url), 0o600)
}

func clearInstanceURL() { _ = os.Remove(instanceFile()) }

// logf 同時寫到主控台與設定資料夾下的 panel.log。
// 無主控台的版本（-H windowsgui）靠這個檔案才留下訊息。
func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	_ = os.MkdirAll(ConfigDir(), 0o700)
	f, err := os.OpenFile(filepath.Join(ConfigDir(), "panel.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}
