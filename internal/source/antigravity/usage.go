package antigravity

import (
	"aiusage/internal/desktop"
	"aiusage/internal/model"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	agyUsageTimeout  = 30 * time.Second
	agyUsageMaxBytes = 1 << 20
)

// agy 1.2.2+ only hands its local CSRF token to child processes, so the
// non-interactive /usage report is the supported way to read quota.
func findAgyCommand() (string, error) {
	names := []string{"agy"}
	if runtime.GOOS == "windows" {
		names = []string{"agy.exe", "agy"}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			p := filepath.Join(base, "agy", "bin", "agy.exe")
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("找不到 Antigravity CLI")
}

func readAgyUsage(ctx context.Context, exe string) (*model.Reported, error) {
	dir, err := os.MkdirTemp("", "aiusage-agy-")
	if err != nil {
		return nil, fmt.Errorf("無法建立 Antigravity CLI 暫存目錄")
	}
	defer os.RemoveAll(dir)
	limit := int(agyUsageTimeout/time.Second) - 5
	cmd := exec.CommandContext(ctx, exe, "-p", "/usage", "--output-format", "json", "--print-timeout", fmt.Sprintf("%ds", limit))
	cmd.Dir = dir
	// Keep a failed silent sign-in from opening a browser on every poll.
	cmd.Env = append(os.Environ(), "BROWSER="+filepath.Join(dir, "no-browser"))
	desktop.ConfigureBackgroundProcess(cmd)
	out := &boundedBuffer{max: agyUsageMaxBytes}
	cmd.Stdout = out
	runErr := cmd.Run()
	if out.over {
		return nil, fmt.Errorf("Antigravity CLI 用量報告過大")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("Antigravity CLI 用量報告逾時；請在終端機執行 agy 確認已登入")
	}
	r, err := parseAgyUsage(out.Bytes(), time.Now())
	if err != nil && runErr != nil {
		return nil, fmt.Errorf("Antigravity CLI 用量報告失敗；請在終端機執行 agy 確認已登入")
	}
	if err != nil {
		return nil, err
	}
	r.Source = "Antigravity CLI 用量報告"
	return r, nil
}

func parseAgyUsage(data []byte, now time.Time) (*model.Reported, error) {
	if i := bytes.IndexByte(data, '{'); i > 0 {
		data = data[i:]
	}
	var doc struct {
		Status  string `json:"status"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						ID        string   `json:"id"`
						Name      string   `json:"name"`
						Window    string   `json:"window"`
						Remaining *float64 `json:"remaining_fraction"`
						Reset     string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.NewDecoder(bytes.NewReader(data)).Decode(&doc) != nil {
		return nil, fmt.Errorf("Antigravity CLI 用量報告格式異常")
	}
	if doc.Status != "SUCCESS" || doc.Command.Name != "usage" {
		return nil, fmt.Errorf("Antigravity CLI 未回傳用量報告；請在終端機執行 agy 確認已登入")
	}
	var groups []quotaGroup
	for _, g := range doc.Command.Data.Groups {
		group := quotaGroup{Name: g.Name}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, quotaBucket{ID: b.ID, Name: b.Name, Window: b.Window, Remaining: b.Remaining, Reset: b.Reset})
		}
		groups = append(groups, group)
	}
	return quotaFromGroups(groups, now)
}

type boundedBuffer struct {
	bytes.Buffer
	max  int
	over bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.over || b.Len()+len(p) > b.max {
		b.over = true
		return 0, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}
