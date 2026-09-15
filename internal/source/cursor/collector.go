package cursor

import (
	"os"
	"path/filepath"
	"time"

	"aiusage/internal/model"
)

// Collector owns polling backoff and the last successful account quota.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	next   time.Time
	cached model.Collection
	read   func() (*model.Reported, error)
}

func New() *Collector { return &Collector{read: readCursorUsage} }

func (c *Collector) Policy() model.ProviderPolicy {
	return model.ProviderPolicy{
		Name:              "Cursor IDE / CLI",
		Note:              "透過 IDE 或 CLI 登入取得帳號共享額度，不將兩個來源或團隊支出相加。",
		UnavailableDetail: "請登入 Cursor IDE 或 Cursor CLI 後重新掃描。",
		AccountAmounts:    true,
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
	ide, cli := cursorAuthPaths()
	for _, path := range []string{ide, cli} {
		out.Diagnosis.Roots = append(out.Diagnosis.Roots, filepath.Dir(path))
		if _, statErr := os.Stat(path); statErr == nil {
			out.Stats.Files++
			out.Stats.RootsHit = append(out.Stats.RootsHit, filepath.Dir(path))
		}
	}
	if err != nil {
		out.Stats.Errors = []string{err.Error()}
		c.next = time.Now().Add(5 * time.Minute)
	} else {
		out.Reported = r
	}
	out.Diagnosis.Files, out.Diagnosis.RootsHit, out.Diagnosis.Errors = out.Stats.Files, out.Stats.RootsHit, out.Stats.Errors
	c.cached = out
	return out
}
