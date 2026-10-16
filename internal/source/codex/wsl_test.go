package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestCodexHelperProcess plays a synthetic app-server when launched by fakeCodexCommand.
func TestCodexHelperProcess(t *testing.T) {
	mode := os.Getenv("AIUSAGE_CODEX_HELPER")
	if mode == "" {
		return
	}
	defer os.Exit(0)
	if mode == "absent" {
		return
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		line := in.Text()
		switch {
		case strings.Contains(line, `"initialize"`):
			fmt.Println(`{"id":1,"result":{}}`)
		case strings.Contains(line, `account/rateLimits/read`):
			if mode == "error" {
				fmt.Println(`{"id":2,"error":{"message":"synthetic-private-error"}}`)
			} else {
				fmt.Println(`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":17,"windowDurationMins":300,"resetsAt":1770000000}}}}`)
			}
			return
		}
	}
}

func fakeCodexCommand(modes map[string]string, launched *[]string) codexWSLCommand {
	return func(ctx context.Context, distro, script string) (*exec.Cmd, error) {
		if !strings.Contains(script, "/mnt/*") {
			return nil, errors.New("script must skip Windows shims")
		}
		*launched = append(*launched, distro)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestCodexHelperProcess")
		cmd.Env = append(os.Environ(), "AIUSAGE_CODEX_HELPER="+modes[distro])
		return cmd, nil
	}
}

func TestCodexWSLFallback(t *testing.T) {
	var launched []string
	list := func(context.Context) ([]string, error) { return []string{"Alpine", "Ubuntu"}, nil }
	r, err := readCodexWSL(context.Background(), list, fakeCodexCommand(map[string]string{"Alpine": "absent", "Ubuntu": "ok"}, &launched), 5*time.Second)
	if err != nil || r == nil || *r.PercentUsed != 17 || !strings.Contains(r.Source, "Ubuntu") || len(launched) != 2 {
		t.Fatalf("WSL fallback: %+v %v %v", r, err, launched)
	}
}

func TestCodexWSLAbsentAndFailures(t *testing.T) {
	var launched []string
	list := func(context.Context) ([]string, error) { return []string{"Ubuntu"}, nil }
	if r, err := readCodexWSL(context.Background(), list, fakeCodexCommand(map[string]string{"Ubuntu": "absent"}, &launched), 5*time.Second); r != nil || err != nil {
		t.Fatalf("missing codex must be silent: %v", err)
	}
	r, err := readCodexWSL(context.Background(), list, fakeCodexCommand(map[string]string{"Ubuntu": "error"}, &launched), 5*time.Second)
	if r != nil || err == nil || strings.Contains(err.Error(), "synthetic-private-error") {
		t.Fatalf("failure must be sanitized: %v", err)
	}
	_, err = readCodexWSL(context.Background(), func(context.Context) ([]string, error) {
		return nil, errors.New("synthetic-private-list")
	}, nil, time.Second)
	if err == nil || strings.Contains(err.Error(), "synthetic-private-list") {
		t.Fatal("list failure must be sanitized")
	}
}
