package antigravity

import (
	"math"
	"testing"
	"time"
)

func TestAntigravityGroupedQuota(t *testing.T) {
	data := []byte(`{"response":{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"g-week","window":"weekly","remainingFraction":0.97,"resetTime":"2026-09-16T12:32:28Z"},{"bucketId":"g-5h","window":"5h","remainingFraction":0.999,"resetTime":"2026-09-11T12:50:24Z"}]},{"displayName":"Claude and GPT","buckets":[{"bucketId":"c-week","window":"weekly","remainingFraction":1},{"bucketId":"c-5h","window":"5h","remainingFraction":1}]}]}}`)
	r, err := parseAntigravityQuota(data, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 4 || math.Abs(*r.PercentUsed-3) > 0.00001 || r.Windows[0].Label != "Gemini Models / 7 天" || r.ResetAt == nil {
		t.Fatalf("grouped quota: %+v", r)
	}
	if _, err := parseAntigravityQuota([]byte(`{"response":{"groups":[{"buckets":[{"window":"5h"}]}]}}`), time.Now()); err == nil {
		t.Fatal("missing quota must not become zero")
	}
}

func TestAgyUsageReport(t *testing.T) {
	data := []byte("notice\n" + `{"conversation_id":"","status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[` +
		`{"name":"Group A","buckets":[{"id":"a-week","name":"Weekly","window":"weekly","remaining_fraction":1,"reset_time":"2026-10-23T03:17:41Z"},{"id":"a-5h","name":"Five","window":"5h","remaining_fraction":1,"reset_time":"2026-10-16T08:17:41Z"}]},` +
		`{"name":"Group B","buckets":[{"id":"b-week","name":"Weekly","window":"weekly","remaining_fraction":0.4,"reset_time":"2026-10-22T08:46:09Z"},{"id":"b-bad","window":"5h"}]}]}}}`)
	r, err := parseAgyUsage(data, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 3 || math.Abs(*r.PercentUsed-60) > 0.00001 || r.Windows[0].Label != "Group B / 7 天" || r.ResetAt == nil {
		t.Fatalf("agy usage: %+v", r)
	}
	for _, bad := range []string{
		`{"status":"ERROR","command":{"name":"usage","data":{"groups":[{"name":"A","buckets":[{"window":"5h","remaining_fraction":1}]}]}}}`,
		`{"status":"SUCCESS","response":"model reply"}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[]}}}`,
		`not json`,
	} {
		if _, err := parseAgyUsage([]byte(bad), time.Now()); err == nil {
			t.Fatalf("unusable report accepted: %s", bad)
		}
	}
}

func TestBoundedBufferRejectsOversizedOutput(t *testing.T) {
	b := &boundedBuffer{max: 4}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("de")); err == nil || !b.over {
		t.Fatal("oversized output accepted")
	}
}

func TestAntigravityModelFallback(t *testing.T) {
	r, err := parseAntigravityModels([]byte(`{"userStatus":{"cascadeModelConfigData":{"clientModelConfigs":[{"label":"Model A","quotaInfo":{"remainingFraction":0}},{"label":"Model B","quotaInfo":{"remainingFraction":1}},{"label":"Unavailable"}]}}}`), time.Now())
	if err != nil || len(r.Windows) != 2 || *r.PercentUsed != 100 {
		t.Fatalf("model fallback: %+v %v", r, err)
	}
}
