package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersionCanRepeatWithoutGlobalFlags(t *testing.T) {
	for range 2 {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr.String())
		}
		if !strings.HasPrefix(stdout.String(), "aiusage ") {
			t.Fatalf("unexpected version: %q", stdout.String())
		}
	}
}

func TestRunInvalidArgumentsDoNotStartService(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"--invalid"}, {"panel", "--port", "65536"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
			t.Errorf("args=%v exit=%d", args, code)
		}
		if stderr.Len() == 0 {
			t.Errorf("args=%v missing error", args)
		}
	}
}
