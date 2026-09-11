package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestIncrementalCollectionAndReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	first := `{"requestId":"first","sessionId":"session","prompt":"sensitive prompt content","message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	c := New()
	p := model.ProviderConfig{ID: model.ProviderClaude, Roots: []string{dir}}
	cutoff := time.Now().Add(-time.Hour)
	initial := c.Collect(p, cutoff, true)
	if len(initial.Events) != 1 || initial.Events[0].Key != "claude|u|first" || len(initial.Diagnosis.TokenKeys) == 0 {
		t.Fatalf("initial collection: %+v", initial)
	}
	if repeated := c.Collect(p, cutoff, true); len(repeated.Events) != 0 || repeated.Stats.Lines != 0 {
		t.Fatalf("unchanged file must not replay events: %+v", repeated)
	}
	diagnostic, err := json.Marshal(initial.Diagnosis)
	if err != nil || strings.Contains(string(diagnostic), "sensitive prompt content") {
		t.Fatalf("diagnosis must only expose field names/counts: %s %v", diagnostic, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"requestId":"second","message":{"usage":{"input_tokens":20,"output_tokens":5}}}` + "\n")
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v %v", err, closeErr)
	}
	appended := c.Collect(p, cutoff, true)
	if len(appended.Events) != 1 || appended.Events[0].Key != "claude|u|second" {
		t.Fatalf("append must produce only the new event: %+v", appended)
	}
	c.Reset()
	if rebuilt := c.Collect(p, cutoff, true); len(rebuilt.Events) != 2 {
		t.Fatalf("reset must rebuild from all source records: %+v", rebuilt)
	}
}
