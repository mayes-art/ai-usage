package antigravity

import (
	"time"

	"aiusage/internal/model"
)

// Collector owns the local-service polling interval and last successful quota.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	next   time.Time
	cached model.Collection
	read   func() (*model.Reported, error)
}

func New() *Collector { return &Collector{read: readAntigravityQuota} }

func (c *Collector) Policy() model.ProviderPolicy {
	return model.ProviderPolicy{
		QuotaOnly:         true,
		UnavailableDetail: "請開啟並登入 Antigravity CLI 或 Desktop，然後重新掃描。",
	}
}

func (c *Collector) Reset() { c.next, c.cached = time.Time{}, model.Collection{} }

func (c *Collector) InvalidateQuotaCache() { c.next = time.Time{} }

func (c *Collector) Collect(p model.ProviderConfig, _ time.Time, _ bool) model.Collection {
	if time.Now().Before(c.next) {
		return c.cached
	}
	r, err := c.read()
	c.next = time.Now().Add(time.Minute)
	out := model.Collection{Reported: c.cached.Reported, Diagnosis: model.Diagnosis{Provider: p.ID}}
	if err != nil {
		out.Stats.Errors = []string{err.Error()}
	} else {
		out.Reported = r
	}
	out.Diagnosis.Errors = out.Stats.Errors
	c.cached = out
	return out
}
