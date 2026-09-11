package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const cursorUsageURL = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"

func cursorAuthPaths() (string, string) {
	roaming := os.Getenv("APPDATA")
	if roaming == "" {
		roaming = filepath.Join(userHome(), "AppData", "Roaming")
	}
	cli := filepath.Join(roaming, "Cursor", "auth.json")
	if runtime.GOOS == "darwin" {
		roaming = filepath.Join(userHome(), "Library", "Application Support")
		cli = filepath.Join(userHome(), ".cursor", "auth.json")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		dir := os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(userHome(), ".config")
		}
		cli = filepath.Join(dir, "cursor", "auth.json")
	}
	return filepath.Join(roaming, "Cursor", "User", "globalStorage", "state.vscdb"), cli
}

func readCursorCLIToken(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Cursor CLI 尚無登入資料；請執行 agent login")
	}
	defer f.Close()
	var auth struct {
		AccessToken string `json:"accessToken"`
	}
	if json.NewDecoder(io.LimitReader(f, 65536)).Decode(&auth) != nil || auth.AccessToken == "" {
		return "", fmt.Errorf("Cursor CLI 需要帳號登入才能查詢方案額度")
	}
	return auth.AccessToken, nil
}

func parseCursorUsage(data []byte, now time.Time) (*Reported, error) {
	var result struct {
		BillingCycleEnd json.Number `json:"billingCycleEnd"`
		PlanUsage       *struct {
			IncludedSpend    float64  `json:"includedSpend"`
			Limit            float64  `json:"limit"`
			TotalPercentUsed *float64 `json:"totalPercentUsed"`
			AutoPercentUsed  *float64 `json:"autoPercentUsed"`
			APIPercentUsed   *float64 `json:"apiPercentUsed"`
		} `json:"planUsage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("Cursor 用量回傳格式無法解析")
	}
	p := result.PlanUsage
	if p == nil {
		return nil, fmt.Errorf("Cursor 未回傳個人方案額度；團隊共享支出不等於個人用量")
	}
	r := &Reported{At: now, Metric: "usd"}
	var percent float64
	if p.Limit > 0 {
		used, limit := p.IncludedSpend/100, p.Limit/100
		if used < 0 || math.IsNaN(used) || math.IsInf(used, 0) {
			return nil, fmt.Errorf("Cursor 用量數值無效")
		}
		r.Used, r.Limit = &used, &limit
		percent = math.Min(100, used/limit*100)
	} else if p.TotalPercentUsed != nil {
		percent = *p.TotalPercentUsed
	} else {
		return nil, fmt.Errorf("Cursor 未提供可計算的方案上限或百分比")
	}
	if percent < 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return nil, fmt.Errorf("Cursor 百分比無效")
	}
	r.PercentUsed = &percent
	if n, err := result.BillingCycleEnd.Float64(); err == nil {
		if reset, ok := epochToTime(n); ok {
			r.ResetAt = &reset
		}
	}
	r.Windows = []ReportedWindow{{Label: "帳單週期", PercentUsed: percent, ResetAt: r.ResetAt}}
	for _, window := range []struct {
		label string
		value *float64
	}{{"Auto / Composer", p.AutoPercentUsed}, {"API 模型", p.APIPercentUsed}} {
		if window.value != nil && *window.value >= 0 && !math.IsNaN(*window.value) && !math.IsInf(*window.value, 0) {
			r.Windows = append(r.Windows, ReportedWindow{Label: window.label, PercentUsed: *window.value, ResetAt: r.ResetAt})
		}
	}
	return r, nil
}

func requestCursorUsage(ctx context.Context, client *http.Client, token, source string) (*Reported, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cursorUsageURL, bytes.NewBufferString("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cursor 用量連線失敗或逾時")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("%s 登入已過期或無權限，請重新登入", source)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Cursor 用量服務 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("Cursor 用量回應讀取失敗")
	}
	r, err := parseCursorUsage(data, time.Now())
	if r != nil {
		r.Source = source + " 帳號用量"
	}
	return r, err
}

func readCursorUsage() (*Reported, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ide, cli := cursorAuthPaths()
	var lastErr error
	seen := map[string]bool{}
	for _, source := range []struct {
		name, path string
		read       func(string) (string, error)
	}{{"Cursor IDE", ide, readCursorIDEToken}, {"Cursor CLI", cli, readCursorCLIToken}} {
		token, err := source.read(source.path)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		r, err := requestCursorUsage(ctx, client, token, source.name)
		if err == nil {
			return r, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *Store) scanCursorQuota(p ProviderConfig) {
	s.mu.RLock()
	next := s.cursorNext
	s.mu.RUnlock()
	if time.Now().Before(next) {
		return
	}
	r, err := readCursorUsage()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursorNext = time.Now().Add(time.Minute)
	st := &scanStats{}
	ide, cli := cursorAuthPaths()
	for _, path := range []string{ide, cli} {
		if _, e := os.Stat(path); e == nil {
			st.Files++
			st.RootsHit = append(st.RootsHit, filepath.Dir(path))
		}
	}
	if err != nil {
		st.Errors = []string{err.Error()}
		s.cursorNext = time.Now().Add(5 * time.Minute)
	} else {
		s.reported[p.ID] = r
	}
	s.stats[p.ID] = st
	s.lastScan[p.ID] = time.Now()
}
