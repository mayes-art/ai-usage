package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
	"aiusage/internal/source/logs"
)

// Collector reads Code logs, Desktop usage snapshots and statusline captures.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	logs        *logs.Collector
	reported    *model.Reported
	org         string
	initialized bool
}

func New() *Collector { return &Collector{logs: logs.New()} }

func (c *Collector) Policy() model.ProviderPolicy {
	return model.ProviderPolicy{
		Name:                   "Claude CLI / Desktop",
		Note:                   "額度為帳號共享限制；本機 token 僅包含可讀的 Code 紀錄。Desktop 開啟後才會更新額度快照。",
		UnavailableDetail:      "尚無可用的 Claude 額度快照。請開啟 Desktop 用量頁面；CLI 可透過 claude-statusline 匯入額度。",
		IncludeDiscoveredRoots: true,
	}
}

func (c *Collector) Reset() {
	c.logs.Reset()
	c.reported, c.org, c.initialized = nil, "", false
}

func (c *Collector) Collect(p model.ProviderConfig, cutoff time.Time, collectKeys bool) model.Collection {
	changedOrg := c.initialized && c.org != p.ClaudeOrg
	if changedOrg {
		c.reported = nil
	}
	c.org, c.initialized = p.ClaudeOrg, true
	p.Roots = claudeRoots(p.Roots)
	out := c.logs.Collect(p, cutoff, collectKeys)
	out.ClearReported = changedOrg
	accept := func(r *model.Reported) {
		if r != nil && r.Usable() && (c.reported == nil || !r.At.Before(c.reported.At)) {
			c.reported = r
		}
	}
	accept(out.Reported)
	now := time.Now()
	for _, dir := range claudeDesktopDirs() {
		data, err := readClaudeJSON(filepath.Join(dir, "plan-usage-history.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			out.Stats.Errors = append(out.Stats.Errors, "Claude Desktop: "+err.Error())
			continue
		}
		out.Stats.Files++
		out.Stats.RootsHit = append(out.Stats.RootsHit, dir)
		r, err := parseClaudeHistory(data, p.ClaudeOrg, now)
		if err != nil {
			out.Stats.Errors = append(out.Stats.Errors, err.Error())
			continue
		}
		accept(r)
	}
	// This optional file contains quota only, never prompts or login tokens.
	data, err := readClaudeJSON(filepath.Join(config.ConfigDir(), "claude-quota.json"))
	if err == nil {
		var r model.Reported
		if json.Unmarshal(data, &r) == nil && !r.At.After(now.Add(time.Minute)) {
			accept(&r)
		}
	}
	out.Reported = c.reported
	out.Diagnosis.CLIPath, out.Diagnosis.DesktopDirs = findClaudeCLI(), claudeDesktopDirs()
	out.Diagnosis.Files, out.Diagnosis.RootsHit, out.Diagnosis.Errors = out.Stats.Files, out.Stats.RootsHit, out.Stats.Errors
	return out
}
