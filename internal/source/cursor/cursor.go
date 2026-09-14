package cursor

import (
	"aiusage/internal/config"
	"aiusage/internal/model"
	"aiusage/internal/record"
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
		roaming = filepath.Join(config.UserHome(), "AppData", "Roaming")
	}
	cli := filepath.Join(roaming, "Cursor", "auth.json")
	if runtime.GOOS == "darwin" {
		roaming = filepath.Join(config.UserHome(), "Library", "Application Support")
		cli = filepath.Join(config.UserHome(), ".cursor", "auth.json")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		dir := os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(config.UserHome(), ".config")
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

func parseCursorUsage(data []byte, now time.Time) (*model.Reported, error) {
	var result struct {
		BillingCycleEnd json.Number `json:"billingCycleEnd"`
		PlanUsage       *struct {
			Limit            float64  `json:"limit"`
			TotalPercentUsed *float64 `json:"totalPercentUsed"`
			AutoPercentUsed  *float64 `json:"autoPercentUsed"`
			APIPercentUsed   *float64 `json:"apiPercentUsed"`
			AutoSpend        *float64 `json:"autoSpend"`
			AutoLimit        *float64 `json:"autoLimit"`
			APISpend         *float64 `json:"apiSpend"`
			APILimit         *float64 `json:"apiLimit"`
		} `json:"planUsage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("Cursor 用量回傳格式無法解析")
	}
	p := result.PlanUsage
	if p == nil {
		return nil, fmt.Errorf("Cursor 未回傳個人方案額度；團隊共享支出不等於個人用量")
	}
	r := &model.Reported{At: now, Metric: "usd"}
	// Cursor 3.17: auto* is first-party; api* is third-party.
	r.Groups = []model.ReportedGroup{
		cursorUsageGroup("cursor-model", "Cursor model", p.AutoPercentUsed, p.AutoSpend, p.AutoLimit),
		cursorUsageGroup("other-model", "Other model", p.APIPercentUsed, p.APISpend, p.APILimit),
	}
	// includedSpend / bonusSpend / totalSpend 都是可用額度而非已用金額，只有 *PercentUsed 是用量。
	var percent float64
	if p.TotalPercentUsed != nil {
		percent = *p.TotalPercentUsed
		if percent < 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
			return nil, fmt.Errorf("Cursor 百分比無效")
		}
		if limit := p.Limit / 100; limit > 0 && !math.IsNaN(limit) && !math.IsInf(limit, 0) {
			used := percent / 100 * limit
			r.Used, r.Limit = &used, &limit
		}
		r.PercentUsed = &percent
	} else if !r.Usable() {
		return nil, fmt.Errorf("Cursor 未提供可計算的方案用量百分比")
	}
	if n, err := result.BillingCycleEnd.Float64(); err == nil {
		if reset, ok := record.EpochToTime(n); ok {
			r.ResetAt = &reset
		}
	}
	window := model.ReportedWindow{Label: "帳單週期", ResetAt: r.ResetAt}
	if p.TotalPercentUsed != nil {
		window.PercentUsed = percent
		window.HasPercent = true
	}
	r.Windows = []model.ReportedWindow{window}
	return r, nil
}

func cursorUsageGroup(id, label string, percent, spend, limit *float64) model.ReportedGroup {
	group := model.ReportedGroup{ID: id, Label: label}
	valid := func(value *float64) bool {
		return value != nil && *value >= 0 && !math.IsNaN(*value) && !math.IsInf(*value, 0)
	}
	if valid(spend) {
		usd := *spend / 100
		group.Used = &usd
	}
	if valid(limit) && *limit > 0 {
		usd := *limit / 100
		group.Limit = &usd
	}
	if valid(percent) {
		value := *percent
		group.PercentUsed = &value
	} else if group.Used != nil && group.Limit != nil {
		value := *group.Used / *group.Limit * 100
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			group.PercentUsed = &value
		}
	}
	return group
}

func requestCursorUsage(ctx context.Context, client *http.Client, token, source string) (*model.Reported, error) {
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

func readCursorUsage() (*model.Reported, error) {
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
