package claude

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseClaudeUsage(t *testing.T) {
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	data := []byte(`{
		"five_hour":{"utilization":0,"resets_at":"2026-10-15T05:30:00.123456+00:00"},
		"seven_day":{"utilization":32.5,"resets_at":"2026-10-21T01:00:00Z"},
		"seven_day_opus":{"utilization":12},
		"seven_day_sonnet":{"utilization":100},
		"seven_day_oauth_apps":{"utilization":4},
		"seven_day_cowork":{"utilization":5},
		"extra_usage":{"utilization":99},
		"context_window":{"used_percentage":88}
	}`)
	r, err := parseClaudeUsage(data, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.At.Equal(now) || r.Source != "Claude Code 登入帳號額度" || r.PercentUsed == nil || *r.PercentUsed != 0 || len(r.Windows) != 6 {
		t.Fatalf("wrong quota: %+v", r)
	}
	reset, _ := time.Parse(time.RFC3339Nano, "2026-10-15T05:30:00.123456+00:00")
	if r.ResetAt == nil || !r.ResetAt.Equal(reset) || r.Windows[0].WindowMinutes != 300 || !r.Windows[0].HasPercent {
		t.Fatalf("zero percentage or reset was lost: %+v", r)
	}
	if r.Windows[1].PercentUsed != 32.5 || r.Windows[1].WindowMinutes != 10080 || r.Windows[5].Label != "7 天 Cowork" {
		t.Fatalf("windows changed: %+v", r.Windows)
	}
}

func TestParseClaudeUsageMissingAndInvalidValues(t *testing.T) {
	for _, data := range []string{
		`{}`, `null`, `[]`, `{"five_hour":null}`, `{"five_hour":{}}`,
		`{"five_hour":{"utilization":null}}`, `{"five_hour":{"utilization":-1}}`,
		`{"five_hour":{"utilization":101}}`, `{"five_hour":{"utilization":"50"}}`,
		`{"five_hour":{"utilization":1e999}}`, `{"context_window":{"used_percentage":99}}`,
		`{"five_hour":{"resets_at":"2026-10-15T02:00:00Z"}}`,
	} {
		if r, err := parseClaudeUsage([]byte(data), time.Now()); err == nil || r != nil {
			t.Errorf("missing/invalid quota became a value for %s: %+v", data, r)
		}
	}
	r, err := parseClaudeUsage([]byte(`{"five_hour":null,"seven_day":{"utilization":0,"resets_at":"invalid"},"seven_day_opus":{"utilization":101}}`), time.Now())
	if err != nil || r.PercentUsed == nil || *r.PercentUsed != 0 || r.ResetAt != nil || len(r.Windows) != 1 || r.Windows[0].WindowMinutes != 10080 {
		t.Fatalf("valid fallback window lost: %+v %v", r, err)
	}
}

func TestClaudeCredentialsPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got, want := claudeCredentialsPath(), filepath.Join(home, ".claude", ".credentials.json"); got != want {
		t.Fatalf("default credentials path = %q, want %q", got, want)
	}
	custom := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	if got, want := claudeCredentialsPath(), filepath.Join(custom, ".credentials.json"); got != want {
		t.Fatalf("custom credentials path = %q, want %q", got, want)
	}
}

func TestReadClaudeAccessToken(t *testing.T) {
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	future := strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)
	for _, tc := range []struct {
		name, body, errorContains string
	}{
		{"valid", `{"claudeAiOauth":{"accessToken":"synthetic-token","expiresAt":` + future + `,"scopes":["user:inference","user:profile"]}}`, ""},
		{"optional metadata", `{"claudeAiOauth":{"accessToken":"synthetic-token"}}`, ""},
		{"missing oauth", `{}`, "格式"},
		{"missing token", `{"claudeAiOauth":{}}`, "憑證"},
		{"invalid token", `{"claudeAiOauth":{"accessToken":"synthetic-token\nsecret"}}`, "憑證"},
		{"expired", `{"claudeAiOauth":{"accessToken":"synthetic-token","expiresAt":1}}`, "過期"},
		{"expires now", `{"claudeAiOauth":{"accessToken":"synthetic-token","expiresAt":` + strconv.FormatInt(now.UnixMilli(), 10) + `}}`, "過期"},
		{"missing profile scope", `{"claudeAiOauth":{"accessToken":"synthetic-token","scopes":["user:inference"]}}`, "user:profile"},
		{"empty scopes", `{"claudeAiOauth":{"accessToken":"synthetic-token","scopes":[]}}`, "user:profile"},
		{"invalid json", `{"synthetic-token":`, "格式"},
		{"oversized", strings.Repeat(" ", claudeCredentialsMax+1), "大小"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".credentials.json")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			token, err := readClaudeAccessToken(path, now)
			if tc.errorContains == "" {
				if err != nil || token != "synthetic-token" {
					t.Fatalf("valid credentials not accepted: %v", err)
				}
			} else if err == nil || token != "" || !strings.Contains(err.Error(), tc.errorContains) {
				t.Fatalf("expected %q, got %v", tc.errorContains, err)
			}
			assertClaudeErrorSanitized(t, err, path, "synthetic-token")
		})
	}
	path := filepath.Join(t.TempDir(), "synthetic-private-account", ".credentials.json")
	if token, err := readClaudeAccessToken(path, now); err == nil || token != "" {
		t.Fatal("missing login file accepted")
	} else {
		assertClaudeErrorSanitized(t, err, path, "synthetic-private-account")
	}
}

type claudeTestTransport func(*http.Request) (*http.Response, error)

func (f claudeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func claudeTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestRequestClaudeUsage(t *testing.T) {
	completed := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	requestDone := false
	client := &http.Client{Transport: claudeTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != claudeUsageURL || req.Body != nil {
			t.Fatalf("unexpected usage request: %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer synthetic-token" || req.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Fatal("missing authentication headers")
		}
		requestDone = true
		return claudeTestResponse(200, `{"five_hour":{"utilization":17}}`), nil
	})}
	r, err := requestClaudeUsage(context.Background(), client, "synthetic-token", func() time.Time {
		if !requestDone {
			t.Fatal("quota timestamp was taken before query completed")
		}
		return completed
	})
	if err != nil || r == nil || *r.PercentUsed != 17 || !r.At.Equal(completed) {
		t.Fatalf("wrong live quota: %+v %v", r, err)
	}
}

func TestRequestClaudeUsageHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{{401, "登入已失效"}, {403, "無權"}, {429, "速率限制"}, {500, "HTTP 500"}, {204, "HTTP 204"}} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			client := &http.Client{Transport: claudeTestTransport(func(*http.Request) (*http.Response, error) {
				return claudeTestResponse(tc.status, `{"error":"synthetic-private-body"}`), nil
			})}
			r, err := requestClaudeUsage(context.Background(), client, "synthetic-token", time.Now)
			if r != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected HTTP failure: %+v %v", r, err)
			}
			assertClaudeErrorSanitized(t, err, "synthetic-token", "synthetic-private-body")
			if tc.status == 429 {
				var retry interface{ RetryAfter() time.Duration }
				if !errors.As(err, &retry) || retry.RetryAfter() != 5*time.Minute {
					t.Fatalf("429 has no conservative retry delay: %v", err)
				}
			}
		})
	}
}

func TestClaudeRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"", 5 * time.Minute}, {"0", 5 * time.Minute}, {"-1", 5 * time.Minute},
		{"30", 5 * time.Minute}, {"600", 10 * time.Minute}, {"invalid", 5 * time.Minute},
		{now.Add(20 * time.Minute).Format(http.TimeFormat), 20 * time.Minute},
		{now.Add(-time.Hour).Format(http.TimeFormat), 5 * time.Minute},
		{"9223372036854775807", time.Duration(1<<63 - 1)},
	} {
		client := &http.Client{Transport: claudeTestTransport(func(*http.Request) (*http.Response, error) {
			resp := claudeTestResponse(429, "")
			resp.Header.Set("Retry-After", tc.header)
			return resp, nil
		})}
		_, err := requestClaudeUsage(context.Background(), client, "synthetic-token", func() time.Time { return now })
		var retry interface{ RetryAfter() time.Duration }
		if !errors.As(err, &retry) || retry.RetryAfter() != tc.want {
			t.Errorf("Retry-After %q: %v", tc.header, err)
		}
	}
}

func TestRequestClaudeUsageRejectsRedirect(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: claudeTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != claudeUsageURL {
			t.Fatal("bearer request followed redirect")
		}
		resp := claudeTestResponse(302, "synthetic-private-body")
		resp.Header.Set("Location", "https://redirect.invalid/synthetic-private-path")
		return resp, nil
	})}
	r, err := requestClaudeUsage(context.Background(), client, "synthetic-token", time.Now)
	if r != nil || err == nil || !strings.Contains(err.Error(), "轉址") || calls != 1 {
		t.Fatalf("redirect was not rejected: %+v %v, calls=%d", r, err, calls)
	}
	assertClaudeErrorSanitized(t, err, "synthetic-token", "synthetic-private-path", "synthetic-private-body")
}

func TestClaudeUnavailableRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{{"1200", 20 * time.Minute}, {"0", time.Minute}, {"invalid", time.Minute}, {"", time.Minute}} {
		client := &http.Client{Transport: claudeTestTransport(func(*http.Request) (*http.Response, error) {
			resp := claudeTestResponse(503, "synthetic-private-body")
			resp.Header.Set("Retry-After", tc.header)
			return resp, nil
		})}
		_, err := requestClaudeUsage(context.Background(), client, "synthetic-token", time.Now)
		var retry interface{ RetryAfter() time.Duration }
		if !errors.As(err, &retry) || retry.RetryAfter() != tc.want || !strings.Contains(err.Error(), "HTTP 503") {
			t.Errorf("503 Retry-After %q: %v", tc.header, err)
		}
		assertClaudeErrorSanitized(t, err, "synthetic-token", "synthetic-private-body")
	}
}

func TestRequestClaudeUsageTimeoutAndTransportErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: claudeTestTransport(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	if r, err := requestClaudeUsage(ctx, client, "synthetic-token", time.Now); err == nil || r != nil || !strings.Contains(err.Error(), "逾時") {
		t.Fatalf("timeout was not handled: %+v %v", r, err)
	}
	client.Transport = claudeTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("synthetic-token synthetic-private-network-error")
	})
	_, err := requestClaudeUsage(context.Background(), client, "synthetic-token", time.Now)
	if err == nil {
		t.Fatal("transport failure accepted")
	}
	assertClaudeErrorSanitized(t, err, "synthetic-token", "synthetic-private-network-error")
}

func TestRequestClaudeUsageRejectsInvalidAndOversizedBodies(t *testing.T) {
	for _, body := range []string{
		`{"synthetic-private-body":`,
		strings.Repeat(" ", claudeUsageResponseMax+1),
	} {
		client := &http.Client{Transport: claudeTestTransport(func(*http.Request) (*http.Response, error) {
			return claudeTestResponse(200, body), nil
		})}
		if r, err := requestClaudeUsage(context.Background(), client, "synthetic-token", time.Now); err == nil || r != nil {
			t.Fatal("invalid or oversized response accepted")
		} else {
			assertClaudeErrorSanitized(t, err, "synthetic-token", "synthetic-private-body")
		}
	}
}

func assertClaudeErrorSanitized(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, secret := range secrets {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error contains sensitive input")
		}
	}
}
