package cursor

import (
	"errors"
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestQuotaCacheBackoffAndReset(t *testing.T) {
	c := New()
	p := model.ProviderConfig{ID: model.ProviderCursor}
	percent := 35.0
	reported := &model.Reported{At: time.Now(), PercentUsed: &percent}
	calls := 0
	c.read = func() (*model.Reported, error) {
		calls++
		if calls == 1 {
			return reported, nil
		}
		return nil, errors.New("offline")
	}
	initial := c.Collect(p, time.Time{}, false)
	if initial.Reported != reported {
		t.Fatalf("expected quota: %+v", initial)
	}
	c.Collect(p, time.Time{}, false)
	if calls != 1 {
		t.Fatalf("successful quota should be cached, calls=%d", calls)
	}
	c.next = time.Time{}
	failed := c.Collect(p, time.Time{}, false)
	if calls != 2 || failed.Reported != reported || len(failed.Stats.Errors) != 1 || time.Until(c.next) < 4*time.Minute {
		t.Fatalf("failed refresh must retain report with backoff: %+v", failed)
	}
	c.Reset()
	reset := c.Collect(p, time.Time{}, false)
	if calls != 3 || reset.Reported != nil {
		t.Fatalf("reset must clear cache and allow immediate retry: %+v", reset)
	}
}
