package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
	"aiusage/internal/source/logs"
)

// Collector combines account quota with Code logs and local quota snapshots.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	logs        *logs.Collector
	reported    *model.Reported
	liveReport  *model.Reported
	org         string
	initialized bool
	nextLive    time.Time
	liveError   string
	liveIssue   string
	authPath    string
	authModTime time.Time
	authSize    int64
	failures    uint
	readLive    func(time.Duration) (*model.Reported, error)
	now         func() time.Time
}

func New() *Collector {
	return &Collector{logs: logs.New(), readLive: readClaudeUsage, now: time.Now}
}

func (c *Collector) Policy() model.ProviderPolicy {
	return model.ProviderPolicy{
		DropExpiredWindows:     true,
		PrimaryWindowMinutes:   300,
		PrimaryWindowLabel:     "5 小時",
		ShowWhenReported:       true,
		Name:                   "Claude CLI / Desktop",
		Note:                   "優先使用主機 Claude Code 登入；Windows 登入不可用時改查執行中的 WSL。約每分鐘更新，額度不相加；本機 token 仍為主機可讀紀錄。",
		UnavailableDetail:      "尚無可用的 Claude 額度。請登入 Claude Code，或開啟 Desktop 用量頁面；CLI 也可透過 claude-statusline 匯入。",
		IncludeDiscoveredRoots: true,
	}
}

func (c *Collector) Reset() {
	c.logs.Reset()
	c.reported, c.org, c.initialized = nil, "", false
	c.liveReport = nil
	c.resetLive()
}

func (c *Collector) resetLive() {
	c.nextLive, c.liveError, c.failures = time.Time{}, "", 0
	c.liveIssue = ""
}

func (c *Collector) checkUpdatedLogin() {
	path := claudeCredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if path != c.authPath || !info.ModTime().Equal(c.authModTime) || info.Size() != c.authSize {
		// 只在登入失敗後略過等待，不繞過服務限流。
		if c.liveIssue != "" {
			c.resetLive()
		}
		c.authPath, c.authModTime, c.authSize = path, info.ModTime(), info.Size()
	}
}

func (c *Collector) InvalidateQuotaCache() {
	// 到期重查不可繞過服務的失敗退避；手動重掃仍可重試。
	if c.failures == 0 {
		c.nextLive = time.Time{}
	}
}

func (c *Collector) Collect(p model.ProviderConfig, cutoff time.Time, collectKeys bool) model.Collection {
	changedOrg := c.initialized && c.org != p.ClaudeOrg
	if changedOrg {
		c.reported = nil
		c.liveReport = nil
		c.resetLive()
	}
	c.org, c.initialized = p.ClaudeOrg, true
	p.Roots = claudeRoots(p.Roots)
	out := c.logs.Collect(p, cutoff, collectKeys)
	out.ClearReported = changedOrg
	accept := func(r *model.Reported) {
		if c.liveReport != nil && strings.HasPrefix(c.liveReport.Source, wslQuotaPrefix) && r != c.liveReport {
			return
		}
		if r != nil && r.Usable() && (c.reported == nil || !r.At.Before(c.reported.At)) {
			c.reported = r
		}
	}
	accept(out.Reported)
	now := c.now()
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
	// 登入帳號無法對應指定的 Desktop 組織時，維持原本的快照來源。
	if p.ClaudeOrg == "" {
		c.checkUpdatedLogin()
		if !c.now().Before(c.nextLive) {
			if r := c.collectLive(); r != nil {
				if (c.liveReport != nil && c.liveReport.Source != r.Source) || strings.HasPrefix(r.Source, wslQuotaPrefix) {
					c.reported = nil
					out.ClearReported = true
				}
				c.liveReport = r
				accept(r)
			}
		}
		if c.liveError != "" && len(out.Stats.Errors) < 5 {
			out.Stats.Errors = append(out.Stats.Errors, "Claude 即時額度暫不可用，保留既有快照："+c.liveError)
		}
		out.Stats.QuotaIssue = c.liveIssue
	}
	out.Reported = c.reported
	out.Diagnosis.CLIPath, out.Diagnosis.DesktopDirs = findClaudeCLI(), claudeDesktopDirs()
	out.Diagnosis.Files, out.Diagnosis.RootsHit, out.Diagnosis.Errors = out.Stats.Files, out.Stats.RootsHit, out.Stats.Errors
	return out
}

func (c *Collector) collectLive() *model.Reported {
	r, err := c.readLive(5 * time.Second)
	if err == nil && (r == nil || !r.Usable()) {
		err = errors.New("Claude 未回傳可用的帳號額度")
	}
	delay := time.Minute
	if err != nil {
		if c.failures < 5 {
			c.failures++
		}
		delay = min(time.Minute<<(c.failures-1), 15*time.Minute)
		var retry interface{ RetryAfter() time.Duration }
		if errors.As(err, &retry) {
			delay = max(delay, retry.RetryAfter())
		}
		c.liveError = err.Error()
		c.liveIssue = ""
		var authErr claudeAuthError
		if errors.As(err, &authErr) {
			c.liveIssue = authErr.Error()
		}
		r = nil
	} else {
		c.failures, c.liveError = 0, ""
		c.liveIssue = ""
	}
	c.nextLive = c.now().Add(delay)
	return r
}
