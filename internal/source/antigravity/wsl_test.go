package antigravity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const syntheticAgyReport = `{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"Group A","buckets":[{"id":"a-5h","window":"5h","remaining_fraction":0.75}]}]}}}`

func TestAgyWSLFallback(t *testing.T) {
	list := func(context.Context) ([]string, error) { return []string{"Alpine", "Ubuntu"}, nil }
	var ran []string
	r, err := readAgyWSL(context.Background(), list, func(_ context.Context, distro string, _ int, script string) ([]byte, error) {
		ran = append(ran, distro)
		if !strings.Contains(script, "/mnt/*") {
			t.Fatal("script must skip Windows shims")
		}
		if distro == "Alpine" {
			return nil, nil
		}
		return []byte(syntheticAgyReport), nil
	})
	if err != nil || r == nil || *r.PercentUsed != 25 || !strings.Contains(r.Source, "Ubuntu") || len(ran) != 2 {
		t.Fatalf("WSL fallback: %+v %v %v", r, err, ran)
	}
}

func TestAgyWSLAbsentAndFailures(t *testing.T) {
	none := func(context.Context) ([]string, error) { return nil, nil }
	if r, err := readAgyWSL(context.Background(), none, nil); r != nil || err != nil {
		t.Fatal("no WSL must be silent")
	}
	list := func(context.Context) ([]string, error) { return []string{"Ubuntu"}, nil }
	r, err := readAgyWSL(context.Background(), list, func(context.Context, string, int, string) ([]byte, error) {
		return []byte("synthetic-private-output"), nil
	})
	if r != nil || err == nil || strings.Contains(err.Error(), "synthetic-private-output") {
		t.Fatalf("invalid WSL output must fail without echoing it: %v", err)
	}
	_, err = readAgyWSL(context.Background(), func(context.Context) ([]string, error) {
		return nil, errors.New("synthetic-private-error")
	}, nil)
	if err == nil || strings.Contains(err.Error(), "synthetic-private-error") {
		t.Fatal("list failure must be sanitized")
	}
}
