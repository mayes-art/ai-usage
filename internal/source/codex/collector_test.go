package codex

import (
	"errors"
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestLiveQuotaThrottleErrorAndReset(t *testing.T) {
	c := New()
	p := model.ProviderConfig{ID: model.ProviderCodex}
	percent := 17.0
	reported := &model.Reported{At: time.Now(), PercentUsed: &percent}
	calls := 0
	c.readLive = func(time.Duration) (*model.Reported, error) {
		calls++
		if calls == 1 {
			return reported, nil
		}
		return nil, errors.New("offline")
	}
	first := c.Collect(p, time.Time{}, false)
	if first.Reported != reported {
		t.Fatalf("live report unavailable: %+v", first)
	}
	c.Collect(p, time.Time{}, false)
	if calls != 1 {
		t.Fatalf("live quota must be throttled, calls=%d", calls)
	}
	c.liveAt = time.Time{}
	failed := c.Collect(p, time.Time{}, false)
	if calls != 2 || failed.Reported != reported || len(failed.Stats.Errors) != 1 {
		t.Fatalf("failed refresh must retain last report and surface error: %+v", failed)
	}
	c.Reset()
	reset := c.Collect(p, time.Time{}, false)
	if calls != 3 || reset.Reported != nil {
		t.Fatalf("reset must clear old quota and trigger collection: %+v", reset)
	}
}
