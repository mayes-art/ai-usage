// Package logs incrementally reads local usage records. It only gathers source
// data; event deduplication, cumulative deltas and retention belong to service.
package logs

import (
	"sort"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
	"aiusage/internal/record"
)

// Collector owns file offsets and diagnostic field names for one provider.
// Its owner serializes Collect and Reset calls.
type Collector struct {
	files map[string]*record.FileState
	keys  map[string]int
}

func New() *Collector {
	c := &Collector{}
	c.Reset()
	return c
}

func (c *Collector) Reset() {
	c.files = map[string]*record.FileState{}
	c.keys = map[string]int{}
}

func (c *Collector) Collect(p model.ProviderConfig, cutoff time.Time, collectKeys bool) model.Collection {
	files, roots, errs := record.Discover(p.Roots)
	out := model.Collection{Stats: model.ScanStats{Files: len(files), RootsHit: roots, Errors: errs}}
	for _, path := range files {
		state := c.files[path]
		if state == nil {
			state = &record.FileState{}
		}
		local := *state
		records, mtime, err := record.ReadNew(path, &local, cutoff)
		if err != nil {
			if len(out.Stats.Errors) < 5 {
				out.Stats.Errors = append(out.Stats.Errors, shortPath(path)+": "+err.Error())
			}
			continue
		}
		c.files[path] = &local
		for _, raw := range records {
			out.Stats.Lines++
			event, reported, keys := record.ParseRecord(p.ID, raw, mtime, collectKeys)
			for key, count := range keys {
				if _, exists := c.keys[key]; exists || len(c.keys) < 600 {
					c.keys[key] += count
				}
			}
			if reported != nil && reported.Usable() && (out.Reported == nil || !reported.At.Before(out.Reported.At)) {
				out.Reported = reported
			}
			if event != nil {
				out.Events = append(out.Events, *event)
			}
		}
	}
	// Removed files must not accumulate offsets indefinitely.
	current := make(map[string]bool, len(files))
	for _, path := range files {
		current[path] = true
	}
	for path := range c.files {
		if !current[path] {
			delete(c.files, path)
		}
	}
	d := model.Diagnosis{Provider: p.ID}
	for _, root := range p.Roots {
		d.Roots = append(d.Roots, config.ExpandVars(root))
	}
	for path, count := range c.keys {
		if record.MatchTokenField(record.LastSeg(path)) != nil {
			d.TokenKeys = append(d.TokenKeys, path)
		}
		if record.LooksQuota(record.LastSeg(path)) {
			if d.QuotaKeys == nil {
				d.QuotaKeys = map[string]any{}
			}
			d.QuotaKeys[path] = count
		}
	}
	sort.Strings(d.TokenKeys)
	if len(d.TokenKeys) > 30 {
		d.TokenKeys = d.TokenKeys[:30]
	}
	d.Files, d.Lines, d.RootsHit, d.Errors = out.Stats.Files, out.Stats.Lines, out.Stats.RootsHit, out.Stats.Errors
	out.Diagnosis = d
	return out
}

func shortPath(path string) string {
	if len(path) <= 64 {
		return path
	}
	return "..." + path[len(path)-61:]
}
