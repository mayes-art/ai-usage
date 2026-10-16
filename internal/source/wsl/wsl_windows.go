package wsl

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"aiusage/internal/desktop"
)

func executable() string { return filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe") }

// Run executes wsl.exe with fixed arguments; stdout is bounded and stderr is discarded.
func Run(ctx context.Context, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable(), args...)
	desktop.ConfigureBackgroundProcess(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	out := &output{limit: limit}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("WSL 指令失敗或逾時")
	}
	return out.data, nil
}

// List returns running distributions without starting stopped ones.
func List(ctx context.Context) ([]string, error) {
	if _, err := os.Stat(executable()); os.IsNotExist(err) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := Run(ctx, 64<<10, "--list", "--running", "--quiet")
	if err != nil {
		return nil, err
	}
	return ParseDistros(data), nil
}

// Exec runs a fixed sh script as the distribution's default user without loading shell profiles.
func Exec(ctx context.Context, distro string, limit int, script string) ([]byte, error) {
	return Run(ctx, limit, "--distribution", distro, "--exec", "sh", "-c", script)
}

// Command prepares a fixed sh script for callers that need interactive stdio.
func Command(ctx context.Context, distro, script string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, executable(), "--distribution", distro, "--exec", "sh", "-c", script)
	desktop.ConfigureBackgroundProcess(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	return cmd, nil
}
