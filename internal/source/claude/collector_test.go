package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
)

func isolatedCollector(t *testing.T) (*Collector, model.ProviderConfig, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(name, dir)
	}
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	c := New()
	c.now = func() time.Time { return now }
	c.readLive = func(time.Duration) (*model.Reported, error) {
		return nil, errors.New("synthetic offline")
	}
	p := model.ProviderConfig{ID: model.ProviderClaude, Enabled: true, Roots: []string{filepath.Join(dir, "projects")}}
	return c, p, &now
}

func quotaSample(at time.Time, percent float64) *model.Reported {
	return &model.Reported{At: at, PercentUsed: &percent, Source: "synthetic account"}
}

func writeQuotaSample(t *testing.T, r *model.Reported) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.ConfigDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.ConfigDir(), "claude-quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorPollsAccountAndKeepsNewerLocalQuota(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls := 0
	c.readLive = func(timeout time.Duration) (*model.Reported, error) {
		calls++
		if timeout != 5*time.Second {
			t.Fatalf("unbounded query timeout: %v", timeout)
		}
		return quotaSample(*now, 24), nil
	}
	writeQuotaSample(t, quotaSample(now.Add(-time.Hour), 10))
	first := c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 1 || first.Reported == nil || *first.Reported.PercentUsed != 24 {
		t.Fatalf("fresh account quota not selected: %+v, calls=%d", first.Reported, calls)
	}
	*now = now.Add(3 * time.Second)
	local := quotaSample(*now, 26)
	local.Source = "synthetic statusline"
	writeQuotaSample(t, local)
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 1 || out.Reported.Source != local.Source || *out.Reported.PercentUsed != 26 {
		t.Fatalf("poll cache blocked newer local quota: %+v, calls=%d", out.Reported, calls)
	}
	if *first.Reported.PercentUsed != 24 {
		t.Fatal("previous collection was mutated")
	}
	*now = now.Add(57 * time.Second)
	out = c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 2 || *out.Reported.PercentUsed != 24 || !out.Reported.At.Equal(*now) {
		t.Fatalf("account was not refreshed at interval: %+v, calls=%d", out.Reported, calls)
	}
}

func TestCollectorPrefersNewestDesktopSnapshot(t *testing.T) {
	c, p, now := isolatedCollector(t)
	dir := claudeDesktopDirs()[0]
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"version":2,"samples":[{"t":%d,"org":"synthetic","u":{"fh":41,"sd":65}}]}`, now.UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, "plan-usage-history.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c.readLive = func(time.Duration) (*model.Reported, error) {
		return quotaSample(now.Add(-time.Minute), 20), nil
	}
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if out.Reported == nil || *out.Reported.PercentUsed != 41 || !out.Reported.At.Equal(*now) {
		t.Fatalf("older query overwrote desktop sample: %+v", out.Reported)
	}
}

func TestCollectorFailureBackoffAndRecovery(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls, failed := 0, false
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		if failed {
			return nil, errors.New("synthetic offline")
		}
		return quotaSample(*now, 0), nil
	}
	first := c.Collect(p, now.Add(-24*time.Hour), false)
	if first.Reported == nil || *first.Reported.PercentUsed != 0 {
		t.Fatal("valid zero quota lost")
	}
	failed = true
	for _, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		*now = c.nextLive
		out := c.Collect(p, now.Add(-24*time.Hour), false)
		if out.Reported != first.Reported || !out.Reported.At.Equal(first.Reported.At) || len(out.Stats.Errors) == 0 {
			t.Fatal("failed query must retain original sample and report error")
		}
		if c.nextLive.Sub(*now) != delay {
			t.Fatalf("retry delay=%v, want %v", c.nextLive.Sub(*now), delay)
		}
		before := calls
		c.InvalidateQuotaCache()
		*now = c.nextLive.Add(-time.Second)
		c.Collect(p, now.Add(-24*time.Hour), false)
		if calls != before {
			t.Fatal("reset boundary bypassed retry backoff")
		}
	}
	failed = false
	*now = c.nextLive
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if len(out.Stats.Errors) != 0 || c.failures != 0 || c.nextLive.Sub(*now) != time.Minute || !out.Reported.At.Equal(*now) {
		t.Fatalf("recovery failed: %+v", out)
	}
	c.InvalidateQuotaCache()
	before := calls
	c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != before+1 {
		t.Fatal("healthy quota reset must query immediately")
	}
}

type syntheticRetryError time.Duration

func (syntheticRetryError) Error() string               { return "synthetic throttled" }
func (e syntheticRetryError) RetryAfter() time.Duration { return time.Duration(e) }

func TestCollectorRetryAfterAndManualRescan(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls := 0
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		return nil, fmt.Errorf("wrapped: %w", syntheticRetryError(20*time.Minute))
	}
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if out.Reported != nil || c.nextLive.Sub(*now) != 20*time.Minute {
		t.Fatal("missing quota must remain unknown and Retry-After must be honored")
	}
	c.Reset()
	c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 2 {
		t.Fatal("manual rescan did not retry immediately")
	}
}

func TestCollectorOrgSelectionDoesNotQueryUnverifiedAccount(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls := 0
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		return quotaSample(*now, 15), nil
	}
	c.Collect(p, now.Add(-24*time.Hour), false)
	p.ClaudeOrg = "synthetic-org"
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 1 || !out.ClearReported || out.Reported != nil {
		t.Fatalf("org selection must clear prior account quota without a query: %+v", out)
	}
	p.ClaudeOrg = ""
	out = c.Collect(p, now.Add(-24*time.Hour), false)
	if calls != 2 || !out.ClearReported || out.Reported == nil {
		t.Fatal("returning to account quota must query immediately")
	}
}

func TestCollectorReadsRepeatedStatuslineCapturesDuringBackoff(t *testing.T) {
	c, p, now := isolatedCollector(t)
	for _, percent := range []int{12, 18, 0} {
		var output bytes.Buffer
		input := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":%d,"resets_at":1900000000}},"context_window":{"used_percentage":90}}`, percent)
		if err := CaptureStatusline(strings.NewReader(input), &output); err != nil {
			t.Fatal(err)
		}
		*now = time.Now()
		out := c.Collect(p, now.Add(-24*time.Hour), false)
		if out.Reported == nil || *out.Reported.PercentUsed != float64(percent) || out.Reported.ResetAt == nil {
			t.Fatalf("statusline update missed during account backoff: %+v", out.Reported)
		}
	}
}

func TestCollectorRejectsEmptySuccessAndUsesCompletionForRetry(t *testing.T) {
	c, p, now := isolatedCollector(t)
	started := *now
	c.readLive = func(time.Duration) (*model.Reported, error) {
		*now = now.Add(4 * time.Second)
		return nil, nil
	}
	out := c.Collect(p, now.Add(-24*time.Hour), false)
	if out.Reported != nil || len(out.Stats.Errors) == 0 || c.nextLive.Sub(started) != 64*time.Second {
		t.Fatalf("empty response or completion-based backoff wrong: %+v", out)
	}
}

func TestUpdatedLoginRetriesAuthenticationFailure(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls := 0
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		if calls == 1 {
			return nil, claudeAuthError("synthetic expired login")
		}
		return quotaSample(*now, 12), nil
	}
	out := c.Collect(p, now.Add(-time.Hour), false)
	if out.Stats.QuotaIssue == "" {
		t.Fatal("authentication failure must be actionable")
	}
	c.Collect(p, now.Add(-time.Hour), false)
	if calls != 1 {
		t.Fatal("unchanged login bypassed backoff")
	}
	if err := os.WriteFile(claudeCredentialsPath(), []byte(`{"synthetic":"new login"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out = c.Collect(p, now.Add(-time.Hour), false)
	if calls != 2 || out.Stats.QuotaIssue != "" || out.Reported == nil || *out.Reported.PercentUsed != 12 {
		t.Fatalf("new login did not recover immediately: %+v", out)
	}
}

func TestChangedLoginDoesNotBypassServiceBackoff(t *testing.T) {
	c, p, now := isolatedCollector(t)
	calls := 0
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		return nil, syntheticRetryError(5 * time.Minute)
	}
	out := c.Collect(p, now.Add(-time.Hour), false)
	if out.Stats.QuotaIssue != "" {
		t.Fatal("service outage must not request login")
	}
	if err := os.WriteFile(claudeCredentialsPath(), []byte(`{"synthetic":"new login"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c.Collect(p, now.Add(-time.Hour), false)
	if calls != 1 {
		t.Fatal("login change bypassed service retry delay")
	}
}

func TestWSLQuotaIsNotOverwrittenByHostSnapshots(t *testing.T) {
	c, p, now := isolatedCollector(t)
	c.readLive = func(time.Duration) (*model.Reported, error) {
		r := quotaSample(*now, 12)
		r.Source = wslQuotaPrefix + "Ubuntu）帳號額度"
		return r, nil
	}
	first := c.Collect(p, now.Add(-time.Hour), false)
	*now = now.Add(3 * time.Second)
	writeQuotaSample(t, quotaSample(*now, 80))
	out := c.Collect(p, now.Add(-time.Hour), false)
	if *out.Reported.PercentUsed != 12 || out.Reported != first.Reported {
		t.Fatal("host snapshot replaced selected WSL account")
	}
	c.readLive = func(time.Duration) (*model.Reported, error) { return quotaSample(*now, 20), nil }
	c.InvalidateQuotaCache()
	out = c.Collect(p, now.Add(-time.Hour), false)
	if !out.ClearReported || *out.Reported.PercentUsed != 20 {
		t.Fatal("valid host did not regain priority")
	}
}
