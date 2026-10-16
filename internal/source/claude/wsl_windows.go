package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"aiusage/internal/desktop"
)

func runClaudeWSL(ctx context.Context, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe"), args...)
	desktop.ConfigureBackgroundProcess(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	output := &wslOutput{limit: limit}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, errors.New("WSL 指令失敗或逾時")
	}
	return output.data, nil
}

func listClaudeWSL(ctx context.Context) ([]string, error) {
	if _, err := os.Stat(filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe")); os.IsNotExist(err) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := runClaudeWSL(ctx, 64<<10, "--list", "--running", "--quiet")
	if err != nil {
		return nil, err
	}
	return parseWSLDistros(data), nil
}

func readClaudeWSL(ctx context.Context, distro string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// 固定腳本、不載入 shell profile；憑證只經由受限 stdout 留在記憶體。
	const script = `p="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/.credentials.json"; test -f "$p" || exit 3; exec head -c 1048577 -- "$p"`
	return runClaudeWSL(ctx, claudeCredentialsMax, "--distribution", distro, "--exec", "sh", "-c", script)
}
