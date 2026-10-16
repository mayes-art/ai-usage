package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"aiusage/internal/model"
)

const (
	geminiUsageURL = "https://gemini.google.com/usage"
	geminiRPCURL   = "https://gemini.google.com/_/BardChatUi/data/batchexecute"
	geminiUsageRPC = "jSf9Qc"
	geminiMaxBody  = 4 << 20
)

var errGeminiLogin = errors.New("Gemini 桌面版需要重新登入")

func readGeminiUsage() (*model.Reported, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	header, err := geminiCookieHeader()
	if err != nil {
		return nil, err
	}
	return fetchGeminiUsage(ctx, geminiHTTPClient(), header, time.Now())
}

func geminiHTTPClient() *http.Client {
	return &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errGeminiLogin
	}}
}

func fetchGeminiUsage(ctx context.Context, client *http.Client, header string, now time.Time) (*model.Reported, error) {
	page, err := geminiRequest(ctx, client, header, geminiUsageURL+"?hl=en", nil)
	if err != nil {
		return nil, err
	}
	at := geminiPageValue(page, "SNlM0e")
	if at == "" {
		return nil, fmt.Errorf("Gemini 用量頁缺少驗證資訊，請確認桌面版已登入並能開啟用量限制頁")
	}
	query := url.Values{"rpcids": {geminiUsageRPC}, "source-path": {"/usage"}, "rt": {"c"}, "hl": {"en"}}
	for key, pageKey := range map[string]string{"bl": "cfb2h", "f.sid": "FdrFJe"} {
		if value := geminiPageValue(page, pageKey); value != "" {
			query.Set(key, value)
		}
	}
	payload, _ := json.Marshal([]any{[]any{[]any{geminiUsageRPC, "[]", nil, "generic"}}})
	form := url.Values{"at": {at}, "f.req": {string(payload)}}
	data, err := geminiRequest(ctx, client, header, geminiRPCURL+"?"+query.Encode(), form)
	if err != nil {
		return nil, err
	}
	r, err := parseGeminiUsage(data, now)
	if err != nil {
		return nil, err
	}
	r.Source = "Gemini 桌面版帳號額度"
	return r, nil
}

func geminiPageValue(page []byte, key string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*("(?:[^"\\]|\\.)*")`)
	match := pattern.FindSubmatch(page)
	var value string
	if len(match) == 2 {
		_ = json.Unmarshal(match[1], &value)
	}
	if len(value) > 4096 {
		return ""
	}
	return value
}

func geminiRequest(ctx context.Context, client *http.Client, header, endpoint string, form url.Values) ([]byte, error) {
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("Gemini 用量查詢無法建立")
	}
	req.Header.Set("Cookie", header)
	req.Header.Set("Accept", "text/html,application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
		req.Header.Set("Origin", "https://gemini.google.com")
		req.Header.Set("Referer", geminiUsageURL)
	}
	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		switch {
		case errors.Is(err, errGeminiLogin):
			return nil, errGeminiLogin
		case errors.As(err, &netErr) && netErr.Timeout():
			return nil, fmt.Errorf("Gemini 用量查詢逾時，請稍後重新掃描")
		default:
			return nil, fmt.Errorf("Gemini 用量頁無法連線，請確認網路或 Proxy 設定")
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errGeminiLogin
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini 用量查詢 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, geminiMaxBody+1))
	if err != nil || len(data) > geminiMaxBody {
		return nil, fmt.Errorf("Gemini 用量回應無法讀取或超過大小上限")
	}
	return data, nil
}

func parseGeminiUsage(data []byte, now time.Time) (*model.Reported, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), geminiMaxBody)
	for scanner.Scan() {
		var frames [][]json.RawMessage
		if json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), &frames) != nil {
			continue
		}
		for _, frame := range frames {
			if len(frame) < 3 {
				continue
			}
			var kind, rpc, payload string
			_ = json.Unmarshal(frame[0], &kind)
			_ = json.Unmarshal(frame[1], &rpc)
			if kind != "wrb.fr" || rpc != geminiUsageRPC || json.Unmarshal(frame[2], &payload) != nil {
				continue
			}
			if r := geminiQuotaPayload([]byte(payload), now); r != nil {
				return r, nil
			}
		}
	}
	return nil, fmt.Errorf("Gemini 未回傳可讀的帳號額度，請確認桌面版的用量限制頁可用")
}

func geminiQuotaPayload(data []byte, now time.Time) *model.Reported {
	var payload []json.RawMessage
	if json.Unmarshal(data, &payload) != nil || len(payload) < 2 {
		return nil
	}
	var buckets [][]json.RawMessage
	if json.Unmarshal(payload[1], &buckets) != nil {
		return nil
	}
	r := &model.Reported{At: now, Metric: "quota"}
	seen := map[int]bool{}
	for _, bucket := range buckets {
		if len(bucket) < 3 {
			continue
		}
		var kind int
		_ = json.Unmarshal(bucket[2], &kind)
		if kind != 1 && kind != 2 || seen[kind] {
			continue
		}
		var fraction *float64
		if json.Unmarshal(bucket[1], &fraction) != nil || fraction == nil || math.IsNaN(*fraction) || math.IsInf(*fraction, 0) || *fraction < 0 || *fraction > 1 {
			continue
		}
		w := model.ReportedWindow{Label: "5 小時", WindowMinutes: 300, PercentUsed: *fraction * 100, HasPercent: true}
		if kind == 2 {
			w.Label, w.WindowMinutes = "7 天", 10080
		}
		if len(bucket) > 3 {
			w.ResetAt = geminiResetTime(bucket[3])
		}
		r.Windows = append(r.Windows, w)
		seen[kind] = true
	}
	if len(r.Windows) == 0 {
		return nil
	}
	sort.SliceStable(r.Windows, func(i, j int) bool { return r.Windows[i].PercentUsed > r.Windows[j].PercentUsed })
	r.PercentUsed, r.ResetAt = &r.Windows[0].PercentUsed, r.Windows[0].ResetAt
	return r
}

func geminiResetTime(data []byte) *time.Time {
	var wrapper []json.RawMessage
	if json.Unmarshal(data, &wrapper) != nil || len(wrapper) == 0 {
		return nil
	}
	var timestamp []json.RawMessage
	if json.Unmarshal(wrapper[0], &timestamp) != nil || len(timestamp) == 0 {
		return nil
	}
	seconds, err := strconv.ParseInt(strings.Trim(string(timestamp[0]), `"`), 10, 64)
	if err != nil || seconds <= 0 || seconds > 253402300799 {
		return nil
	}
	t := time.Unix(seconds, 0)
	return &t
}

func geminiCookieAllowed(host, name string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), ".")
	if host != "google.com" && host != "gemini.google.com" {
		return false
	}
	switch name {
	case "SID", "HSID", "SSID", "APISID", "SAPISID", "NID", "SIDCC", "OSID",
		"__Secure-1PSID", "__Secure-3PSID", "__Secure-1PSIDTS", "__Secure-3PSIDTS",
		"__Secure-1PAPISID", "__Secure-3PAPISID", "__Secure-1PSIDCC", "__Secure-3PSIDCC",
		"__Secure-OSID", "__Secure-BUCKET":
		return true
	default:
		return false
	}
}
