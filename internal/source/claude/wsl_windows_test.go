package claude

import (
	"context"
	"os"
	"testing"
	"time"

	"aiusage/internal/source/wsl"
)

func TestWSLBridgeSyntheticOnly(t *testing.T) {
	if os.Getenv("AIUSAGE_TEST_WSL") != "1" {
		t.Skip("opt-in WSL bridge check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	distros, err := wsl.List(ctx)
	if err != nil || len(distros) == 0 {
		t.Fatalf("running WSL required: %v", err)
	}
	const script = `printf '%s' '{"claudeAiOauth":{"accessToken":"synthetic-wsl"}}'`
	data, err := wsl.Exec(ctx, distros[0], claudeCredentialsMax, script)
	if err != nil {
		t.Fatal(err)
	}
	token, err := parseClaudeAccessToken(data, time.Now())
	if err != nil || token != "synthetic-wsl" {
		t.Fatal("WSL command output was not transferred intact")
	}
}
