package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// readCodexRateLimits asks the installed Codex CLI for the account limits used
// by Codex Desktop. Session JSONL remains the fallback when the CLI is absent.
func readCodexRateLimits(timeout time.Duration) (*Reported, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	exe, err := findCodexCommand()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, exe, "app-server", "--stdio")
	configureBackgroundProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Codex stdout: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start %s: %w", exe, err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	enc := json.NewEncoder(stdin)
	if err := enc.Encode(map[string]any{
		"method": "initialize", "id": 1,
		"params": map[string]any{"clientInfo": map[string]string{
			"name": "aiusage", "title": "AI Usage", "version": version,
		}},
	}); err != nil {
		return nil, fmt.Errorf("send initialize: %w", err)
	}

	dec := json.NewDecoder(bufio.NewReader(stdout))
	for {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("Codex app-server timed out after %s", timeout)
			}
			if err == io.EOF && stderr.Len() > 0 {
				return nil, fmt.Errorf("Codex app-server stopped: %s", oneLine(stderr.String()))
			}
			return nil, fmt.Errorf("read Codex app-server: %w", err)
		}
		if string(msg.ID) != "1" {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return nil, fmt.Errorf("Codex initialize error: %s", msg.Error)
		}
		break
	}

	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, fmt.Errorf("send initialized: %w", err)
	}
	if err := enc.Encode(map[string]any{
		"method": "account/rateLimits/read", "id": 2, "params": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("request Codex rate limits: %w", err)
	}

	for {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("Codex rate-limit request timed out after %s", timeout)
			}
			if stderr.Len() > 0 {
				return nil, fmt.Errorf("Codex app-server stopped: %s", oneLine(stderr.String()))
			}
			return nil, fmt.Errorf("read Codex rate limits: %w", err)
		}
		if string(msg.ID) != "2" {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return nil, fmt.Errorf("Codex rate-limit error: %s", msg.Error)
		}
		var node any
		decResult := json.NewDecoder(strings.NewReader(string(msg.Result)))
		decResult.UseNumber()
		if err := decResult.Decode(&node); err != nil {
			return nil, fmt.Errorf("decode Codex rate limits: %w", err)
		}
		_, extract := ExtractRecord(node, false)
		reported := buildReported(extract, time.Now())
		if reported == nil || !reported.usable() {
			return nil, fmt.Errorf("Codex returned no usable rate-limit windows")
		}
		return reported, nil
	}
}

func findCodexCommand() (string, error) {
	// npm creates codex.cmd on Windows. LookPath("codex") does not reliably
	// resolve PowerShell shims when launched from a GUI executable.
	names := []string{"codex"}
	if runtime.GOOS == "windows" {
		names = []string{"codex.exe", "codex.cmd", "codex"}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("Codex CLI not found in PATH (install it or add codex.cmd to PATH)")
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}
