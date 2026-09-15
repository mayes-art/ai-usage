package codex

import (
	"time"

	"aiusage/internal/model"
	"aiusage/internal/source/logs"
)

// Collector combines incremental session logs with the CLI's live quota.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	logs      *logs.Collector
	liveAt    time.Time
	live      *model.Reported
	liveError string
	readLive  func(time.Duration) (*model.Reported, error)
}

func New() *Collector {
	return &Collector{logs: logs.New(), readLive: readCodexRateLimits}
}

func (c *Collector) Reset() {
	c.logs.Reset()
	c.liveAt, c.live, c.liveError = time.Time{}, nil, ""
}

func (c *Collector) InvalidateQuotaCache() { c.liveAt = time.Time{} }

func (c *Collector) Collect(p model.ProviderConfig, cutoff time.Time, collectKeys bool) model.Collection {
	out := c.logs.Collect(p, cutoff, collectKeys)
	if time.Since(c.liveAt) >= 30*time.Second {
		reported, err := c.readLive(8 * time.Second)
		c.liveAt = time.Now()
		if err != nil {
			c.liveError = err.Error()
		} else {
			c.live, c.liveError = reported, ""
		}
	}
	if c.live != nil && (out.Reported == nil || !c.live.At.Before(out.Reported.At)) {
		out.Reported = c.live
	}
	if c.liveError != "" && len(out.Stats.Errors) < 5 {
		out.Stats.Errors = append(out.Stats.Errors, "Live quota unavailable; using session fallback: "+c.liveError)
	}
	out.Diagnosis.Errors = out.Stats.Errors
	return out
}
