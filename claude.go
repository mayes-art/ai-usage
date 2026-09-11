package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func findClaudeCLI() string {
	for _, name := range []string{"claude.exe", "claude.cmd", "claude"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	local := filepath.Join(userHome(), ".local", "bin", "claude.exe")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	for _, dir := range claudeDesktopDirs() {
		bundled, _ := filepath.Glob(filepath.Join(dir, "claude-code", "*", "claude.exe"))
		if len(bundled) > 0 {
			return bundled[len(bundled)-1]
		}
	}
	return ""
}

// Only inspect known usage files, never Desktop cookies or login databases.
func claudeDesktopDirs() []string {
	if runtime.GOOS == "darwin" {
		return []string{filepath.Join(userHome(), "Library", "Application Support", "Claude")}
	}
	dirs := []string{filepath.Join(userHome(), "AppData", "Roaming", "Claude")}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		dirs = append(dirs, filepath.Join(appdata, "Claude"))
	}
	packages, _ := filepath.Glob(filepath.Join(userHome(), "AppData", "Local", "Packages", "Claude_*", "LocalCache", "Roaming", "Claude"))
	dirs = append(dirs, packages...)
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		if !seen[dir] {
			out = append(out, dir)
			seen[dir] = true
		}
	}
	return out
}

func claudeRoots(roots []string) []string {
	out := append([]string{}, roots...)
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		out = append(out, filepath.Join(dir, "projects"))
	}
	for _, dir := range claudeDesktopDirs() {
		out = append(out, filepath.Join(dir, "claude-code-sessions"))
	}
	return out
}

func readClaudeJSON(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("Claude usage file exceeds 8 MB")
	}
	return data, err
}

func parseClaudeHistory(data []byte, org string, now time.Time) (*Reported, error) {
	var doc struct {
		Version int `json:"version"`
		Samples []struct {
			T   int64               `json:"t"`
			Org string              `json:"org"`
			U   map[string]*float64 `json:"u"`
			FH  *float64            `json:"fh"`
			SD  *float64            `json:"sd"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("Claude Desktop usage JSON: %w", err)
	}
	if doc.Version != 1 && doc.Version != 2 {
		return nil, fmt.Errorf("unsupported Claude Desktop usage version %d", doc.Version)
	}
	latest := -1
	for i, sample := range doc.Samples {
		if org != "" && sample.Org != org {
			continue
		}
		if sample.T <= 0 || time.UnixMilli(sample.T).After(now.Add(time.Minute)) {
			continue
		}
		if latest < 0 || sample.T >= doc.Samples[latest].T {
			latest = i
		}
	}
	if latest < 0 {
		return nil, fmt.Errorf("Claude Desktop 尚無此帳號的額度紀錄")
	}
	sample := doc.Samples[latest]
	values := sample.U
	if doc.Version == 1 {
		values = map[string]*float64{"fh": sample.FH, "sd": sample.SD}
	}
	r := &Reported{At: time.UnixMilli(sample.T), Source: "Claude Desktop 額度快照"}
	for _, spec := range []struct {
		key, label string
		mins       float64
	}{
		{"fh", "5 小時", 300}, {"sd", "7 天", 10080},
		{"so", "7 天 Opus", 10080}, {"sn", "7 天 Sonnet", 10080},
		{"oa", "7 天 OAuth apps", 10080}, {"cw", "7 天 Cowork", 10080},
	} {
		v := values[spec.key]
		if v == nil || *v < 0 || *v > 100 || math.IsNaN(*v) || math.IsInf(*v, 0) {
			continue
		}
		r.Windows = append(r.Windows, ReportedWindow{Label: spec.label, WindowMinutes: spec.mins, PercentUsed: *v, hasPercent: true})
	}
	if len(r.Windows) == 0 {
		return nil, fmt.Errorf("Claude Desktop 最新快照沒有可用額度，請開啟 Claude 的用量頁面")
	}
	r.PercentUsed = &r.Windows[0].PercentUsed
	return r, nil
}

// Read only the rate_limits object. A context_window percentage is not quota.
func parseClaudeQuota(node any, at time.Time) *Reported {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	limits, ok := m["rate_limits"].(map[string]any)
	if !ok {
		return nil
	}
	r := &Reported{At: at, Source: "Claude CLI 額度紀錄"}
	for _, spec := range []struct {
		key  string
		mins float64
	}{{"five_hour", 300}, {"seven_day", 10080}} {
		w, ok := limits[spec.key].(map[string]any)
		if !ok {
			continue
		}
		v, valid := toNumber(w["used_percentage"])
		if !valid {
			v, valid = toNumber(w["utilization"])
		}
		if !valid || v < 0 || v > 100 {
			continue
		}
		win := ReportedWindow{Label: windowLabel(spec.mins), WindowMinutes: spec.mins, PercentUsed: v, hasPercent: true}
		if n, ok := toNumber(w["resets_at"]); ok {
			t, valid := epochToTime(n)
			if valid {
				win.ResetAt = &t
			}
		}
		if str, ok := w["resets_at"].(string); ok {
			t, valid := parseTimeString(str)
			if valid {
				win.ResetAt = &t
			}
		}
		r.Windows = append(r.Windows, win)
	}
	if len(r.Windows) == 0 {
		_, ctx := ExtractRecord(limits, false)
		r = buildReported(ctx, at)
		if r == nil || !r.usable() {
			return nil
		}
		r.Source = "Claude CLI 額度紀錄"
		return r
	}
	r.PercentUsed, r.ResetAt = &r.Windows[0].PercentUsed, r.Windows[0].ResetAt
	return r
}

func (s *Store) scanClaudeQuota(p ProviderConfig, st *scanStats) {
	s.mu.Lock()
	if cur := s.reported[p.ID]; cur != nil && cur.Source == "Claude Desktop 額度快照" {
		delete(s.reported, p.ID)
	}
	s.mu.Unlock()
	for _, dir := range claudeDesktopDirs() {
		path := filepath.Join(dir, "plan-usage-history.json")
		data, err := readClaudeJSON(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			st.Errors = append(st.Errors, "Claude Desktop: "+err.Error())
			continue
		}
		st.Files++
		st.RootsHit = append(st.RootsHit, dir)
		r, err := parseClaudeHistory(data, p.ClaudeOrg, time.Now())
		if err != nil {
			st.Errors = append(st.Errors, err.Error())
			continue
		}
		s.mu.Lock()
		if cur := s.reported[p.ID]; cur == nil || !r.At.Before(cur.At) {
			s.reported[p.ID] = r
		}
		s.mu.Unlock()
	}
	// The optional status-line command saves quota only, without prompts or tokens.
	data, err := readClaudeJSON(filepath.Join(ConfigDir(), "claude-quota.json"))
	if err == nil {
		var r Reported
		if json.Unmarshal(data, &r) == nil && r.usable() && !r.At.After(time.Now().Add(time.Minute)) {
			s.mu.Lock()
			if cur := s.reported[p.ID]; cur == nil || r.At.After(cur.At) {
				s.reported[p.ID] = &r
			}
			s.mu.Unlock()
		}
	}
}

func captureClaudeStatusline() error {
	var node any
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	dec.UseNumber()
	if err := dec.Decode(&node); err != nil {
		return err
	}
	r := parseClaudeQuota(node, time.Now())
	if r == nil {
		return nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(ConfigDir(), "claude-quota-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(ConfigDir(), "claude-quota.json")); err != nil {
		return err
	}
	fmt.Printf("Claude %.0f%%", *r.PercentUsed)
	return nil
}
