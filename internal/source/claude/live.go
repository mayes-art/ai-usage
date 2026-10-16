package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
)

const (
	claudeUsageURL         = "https://api.anthropic.com/api/oauth/usage"
	claudeCredentialsMax   = 1 << 20
	claudeUsageResponseMax = 1 << 20
)

type claudeAuthError string

func (e claudeAuthError) Error() string { return string(e) }

func claudeCredentialsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(config.UserHome(), ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

func readClaudeAccessToken(path string, now time.Time) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", claudeAuthError("尚無可讀的 Claude Code 登入資料，請先登入 Claude Code")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, claudeCredentialsMax+1))
	if err != nil || len(data) > claudeCredentialsMax {
		return "", errors.New("Claude Code 登入資料讀取失敗或超過大小限制")
	}
	return parseClaudeAccessToken(data, now)
}

func parseClaudeAccessToken(data []byte, now time.Time) (string, error) {
	if len(data) > claudeCredentialsMax {
		return "", errors.New("Claude Code 登入資料超過大小限制")
	}
	var credentials struct {
		OAuth *struct {
			AccessToken string    `json:"accessToken"`
			ExpiresAt   *int64    `json:"expiresAt"`
			Scopes      *[]string `json:"scopes"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &credentials) != nil || credentials.OAuth == nil {
		return "", claudeAuthError("Claude Code 登入資料格式無效，請重新登入")
	}
	auth := credentials.OAuth
	if auth.AccessToken == "" || strings.ContainsAny(auth.AccessToken, " \t\r\n") {
		return "", claudeAuthError("Claude Code 尚無可用的帳號登入憑證，請重新登入")
	}
	if auth.ExpiresAt != nil && !time.UnixMilli(*auth.ExpiresAt).After(now) {
		return "", claudeAuthError("Claude Code 登入已過期，請執行 claude auth login 更新登入")
	}
	if auth.Scopes != nil {
		profile := false
		for _, scope := range *auth.Scopes {
			profile = profile || scope == "user:profile"
		}
		if !profile {
			return "", claudeAuthError("Claude Code 登入缺少 user:profile 權限，請重新登入帳號")
		}
	}
	return auth.AccessToken, nil
}

func readClaudeUsage(timeout time.Duration) (*model.Reported, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return readClaudeEnvironments(ctx, &http.Client{Timeout: timeout}, func() (string, error) {
		return readClaudeAccessToken(claudeCredentialsPath(), time.Now())
	}, listClaudeWSL, readClaudeWSL)
}

func requestClaudeUsage(ctx context.Context, client *http.Client, token string, now func() time.Time) (*model.Reported, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	if err != nil {
		return nil, errors.New("Claude 額度查詢無法建立")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return nil, errors.New("Claude 額度連線失敗或逾時")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, claudeAuthError("Claude Code 登入已失效，請執行 claude auth login 更新登入")
	case resp.StatusCode == http.StatusForbidden:
		return nil, claudeAuthError("Claude Code 登入無權查詢帳號額度，請確認訂閱帳號與登入權限")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &claudeUsageError{statusCode: resp.StatusCode, retryAfter: claudeRetryAfter(resp.Header.Get("Retry-After"), now(), 5*time.Minute)}
	case resp.StatusCode == http.StatusServiceUnavailable:
		return nil, &claudeUsageError{statusCode: resp.StatusCode, retryAfter: claudeRetryAfter(resp.Header.Get("Retry-After"), now(), time.Minute)}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, errors.New("Claude 額度服務要求轉址，已停止查詢")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("Claude 額度服務 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, claudeUsageResponseMax+1))
	if err != nil || len(data) > claudeUsageResponseMax {
		return nil, errors.New("Claude 額度回應讀取失敗或超過大小限制")
	}
	return parseClaudeUsage(data, now())
}

type claudeUsageError struct {
	statusCode int
	retryAfter time.Duration
}

func (e *claudeUsageError) Error() string {
	if e.statusCode == http.StatusTooManyRequests {
		return "Claude 額度查詢受到速率限制，稍後自動重試"
	}
	return fmt.Sprintf("Claude 額度服務暫時無法使用（HTTP %d），稍後自動重試", e.statusCode)
}
func (e *claudeUsageError) RetryAfter() time.Duration { return e.retryAfter }

func claudeRetryAfter(value string, now time.Time, delay time.Duration) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		if seconds > 0 {
			delay = max(delay, time.Duration(seconds)*time.Second)
		}
	} else if at, err := http.ParseTime(value); err == nil {
		delay = max(delay, at.Sub(now))
	}
	return delay
}

func parseClaudeUsage(data []byte, now time.Time) (*model.Reported, error) {
	var body map[string]json.RawMessage
	if json.Unmarshal(data, &body) != nil {
		return nil, errors.New("Claude 額度回應格式無法解析")
	}
	r := &model.Reported{At: now, Source: "Claude Code 登入帳號額度"}
	for _, spec := range []struct {
		key, label string
		minutes    float64
	}{
		{"five_hour", "5 小時", 300},
		{"seven_day", "7 天", 10080},
		{"seven_day_opus", "7 天 Opus", 10080},
		{"seven_day_sonnet", "7 天 Sonnet", 10080},
		{"seven_day_oauth_apps", "7 天 OAuth apps", 10080},
		{"seven_day_cowork", "7 天 Cowork", 10080},
	} {
		var window struct {
			Percent *float64 `json:"utilization"`
			ResetAt string   `json:"resets_at"`
		}
		if json.Unmarshal(body[spec.key], &window) != nil || window.Percent == nil {
			continue
		}
		percent := *window.Percent
		if percent < 0 || percent > 100 || math.IsNaN(percent) || math.IsInf(percent, 0) {
			continue
		}
		w := model.ReportedWindow{Label: spec.label, WindowMinutes: spec.minutes, PercentUsed: percent, HasPercent: true}
		if reset, err := time.Parse(time.RFC3339Nano, window.ResetAt); err == nil {
			w.ResetAt = &reset
		}
		r.Windows = append(r.Windows, w)
	}
	if len(r.Windows) == 0 {
		return nil, errors.New("Claude 未回傳可用的帳號額度百分比")
	}
	r.PercentUsed, r.ResetAt = &r.Windows[0].PercentUsed, r.Windows[0].ResetAt
	return r, nil
}
