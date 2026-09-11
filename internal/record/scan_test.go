package record

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aiusage/internal/model"
)

func TestReadNewReadsJSONBeyondChunkBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	// Usage after the old 24 MiB boundary must survive complete-document parsing.
	data := append([]byte(`[{"padding":"`), bytes.Repeat([]byte("x"), readChunkCap+1)...)
	data = append(data, []byte(`"},{"usage":{"input_tokens":12,"output_tokens":3}}]`)...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	state := &FileState{}
	records, mtime, err := ReadNew(path, state, time.Time{})
	if err != nil || len(records) != 2 {
		t.Fatalf("complete JSON read: records=%d err=%v", len(records), err)
	}
	event, _, _ := ParseRecord(model.ProviderClaude, records[1], mtime, false)
	if event == nil || event.Usage.Billable() != 15 {
		t.Fatalf("usage beyond the chunk boundary was lost: %+v", event)
	}
	records, _, err = ReadNew(path, state, time.Time{})
	if err != nil || len(records) != 0 {
		t.Fatalf("unchanged document read twice: records=%d err=%v", len(records), err)
	}
}

func TestReadNewDoesNotMarkInvalidJSONConsumed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.json")
	if err := os.WriteFile(path, []byte(`[{"usage":{"input_tokens":12}}`), 0600); err != nil {
		t.Fatal(err)
	}
	state := &FileState{}
	for i := 0; i < 2; i++ {
		if _, _, err := ReadNew(path, state, time.Time{}); err == nil {
			t.Fatal("incomplete JSON must report an error and remain retryable")
		}
		if state.offset != 0 || state.size != 0 {
			t.Fatalf("invalid document marked consumed: %+v", state)
		}
	}
	if err := os.WriteFile(path, []byte(`[{"usage":{"input_tokens":12}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	records, _, err := ReadNew(path, state, time.Time{})
	if err != nil || len(records) != 1 {
		t.Fatalf("completed document was not retried: records=%d err=%v", len(records), err)
	}
}

func TestReadNewJSONLLeavesPartialLineForNextRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{\"usage\":{\"input_tokens\":1}}\n{\"usage\":"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &FileState{}
	records, _, err := ReadNew(path, state, time.Time{})
	if err != nil || len(records) != 1 {
		t.Fatalf("initial lines: records=%d err=%v", len(records), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("{\"input_tokens\":2}}\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append line: %v %v", writeErr, closeErr)
	}
	records, _, err = ReadNew(path, state, time.Time{})
	if err != nil || len(records) != 1 {
		t.Fatalf("completed lines: records=%d err=%v", len(records), err)
	}
}
