package record

import (
	"aiusage/internal/model"
	"testing"
	"time"
)

var fallback = time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)

func parse(t *testing.T, provider, raw string) *model.Event {
	t.Helper()
	ev, _, _ := ParseRecord(provider, []byte(raw), fallback, false)
	return ev
}

func TestAppServerWindowDurationAndDuplicates(t *testing.T) {
	raw := []byte(`{"rateLimits":{"primary":{"usedPercent":17,"windowDurationMins":300,"resetsAt":1770000000}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":17,"windowDurationMins":300,"resetsAt":1770000000},"secondary":{"usedPercent":6,"windowDurationMins":10080,"resetsAt":1770600000}}}}`)
	_, rep, _ := ParseRecord(model.ProviderCodex, raw, time.Now(), false)
	if rep == nil || rep.PercentUsed == nil || *rep.PercentUsed != 17 {
		t.Fatalf("unexpected report: %#v", rep)
	}
	if len(rep.Windows) != 2 {
		t.Fatalf("want 2 deduplicated windows, got %d: %#v", len(rep.Windows), rep.Windows)
	}
	if rep.Windows[0].Label != "5 小時" || rep.Windows[1].Label != "7 天" {
		t.Fatalf("unexpected labels: %#v", rep.Windows)
	}
}

// Claude Code 風格：assistant 訊息帶 message.usage，含快取分項。
func TestClaudeShape(t *testing.T) {
	raw := `{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-09-11T09:30:00.000Z",
	"requestId":"req_1","cwd":"C:\\work\\proj",
	"message":{"id":"msg_01","role":"assistant","model":"claude-opus-5",
	"usage":{"input_tokens":120,"output_tokens":840,
	"cache_creation_input_tokens":2000,"cache_read_input_tokens":15000}}}`
	ev := parse(t, model.ProviderClaude, raw)
	if ev == nil {
		t.Fatal("沒有解析出事件")
	}
	if ev.Usage.Input != 120 || ev.Usage.Output != 840 {
		t.Errorf("input/output 錯誤: %+v", ev.Usage)
	}
	if ev.Usage.CacheWrite != 2000 || ev.Usage.CacheRead != 15000 {
		t.Errorf("快取分項錯誤: %+v", ev.Usage)
	}
	if got := ev.Usage.Billable(); got != 17960 {
		t.Errorf("Billable = %d, 期望 17960", got)
	}
	if ev.Model != "claude-opus-5" {
		t.Errorf("model = %q", ev.Model)
	}
	if ev.ApproxTime {
		t.Error("應該讀到記錄自身的時間")
	}
	if ev.At.Hour() != 9 || ev.At.Minute() != 30 {
		t.Errorf("時間解析錯誤: %v", ev.At)
	}
	if ev.Key != model.ProviderClaude+"|u|req_1" {
		t.Errorf("去重鍵應優先用 requestId, got %q", ev.Key)
	}
}

// Codex 風格：同時給「本次」與「累計」，只能採計本次，否則嚴重高估。
func TestCodexCumulativeVsDelta(t *testing.T) {
	raw := `{"type":"event_msg","id":"ev9","timestamp":"2026-09-11T09:00:00Z",
	"payload":{"type":"token_count","info":{
	  "total_token_usage":{"input_tokens":50000,"output_tokens":9000,"total_tokens":59000},
	  "last_token_usage":{"input_tokens":1200,"output_tokens":300,"total_tokens":1500},
	  "model_context_window":272000}}}`
	ev := parse(t, model.ProviderCodex, raw)
	if ev == nil {
		t.Fatal("沒有解析出事件")
	}
	if ev.Cumulative {
		t.Error("有 last_token_usage 時不該標為累計")
	}
	if ev.Usage.Input != 1200 || ev.Usage.Output != 300 {
		t.Errorf("應只採計 last_token_usage, got %+v", ev.Usage)
	}
}

// 只有累計值時，必須標記為累計，由 Store 換算增量。
func TestCodexCumulativeOnly(t *testing.T) {
	raw := `{"id":"ev10","sessionId":"sx","timestamp":"2026-09-11T09:05:00Z",
	"info":{"total_token_usage":{"input_tokens":50000,"output_tokens":9000}}}`
	ev := parse(t, model.ProviderCodex, raw)
	if ev == nil || !ev.Cumulative {
		t.Fatalf("應標記為累計: %+v", ev)
	}
}

func TestCodexFlatSubsetFieldsAreNotDoubleCounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int64
	}{
		{"reported total", `{"info":{"last_token_usage":{"input_tokens":82019,"output_tokens":81,"cached_input_tokens":80640,"reasoning_output_tokens":30,"total_tokens":82100}}}`, 82100},
		{"missing total", `{"info":{"last_token_usage":{"input_tokens":82019,"output_tokens":81,"cached_input_tokens":80640,"reasoning_output_tokens":30}}}`, 82100},
		{"cumulative", `{"info":{"total_token_usage":{"input_tokens":1000,"output_tokens":200,"cached_input_tokens":800,"reasoning_output_tokens":100,"total_tokens":1200}}}`, 1200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := parse(t, model.ProviderCodex, tc.raw)
			if event == nil || event.Usage.Billable() != tc.want {
				t.Fatalf("billable tokens: event=%+v want=%d", event, tc.want)
			}
			if event.Usage.CacheRead == 0 || event.Usage.Reasoning == 0 {
				t.Fatalf("token details should remain available: %+v", event.Usage)
			}
		})
	}
}

// OpenAI 風格的 *_details 是上層欄位的子集，不能重複計算。
func TestDetailsNotDoubleCounted(t *testing.T) {
	raw := `{"id":"r1","created":1789000000,"model":"gpt-5-codex",
	"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,
	"prompt_tokens_details":{"cached_tokens":800},
	"completion_tokens_details":{"reasoning_tokens":200}}}`
	ev := parse(t, model.ProviderCodex, raw)
	if ev == nil {
		t.Fatal("沒有解析出事件")
	}
	if ev.Usage.Input != 1000 || ev.Usage.Output != 500 {
		t.Errorf("input/output 錯誤: %+v", ev.Usage)
	}
	if ev.Usage.CacheRead != 0 || ev.Usage.Reasoning != 0 {
		t.Errorf("details 不該被重複計入: %+v", ev.Usage)
	}
}

// Gemini 風格的欄位名稱是駝峰式且叫法不同。
func TestGeminiShape(t *testing.T) {
	raw := `{"sessionId":"g1","time":"2026-09-11T08:00:00Z","modelName":"gemini-2.5-pro",
	"usageMetadata":{"promptTokenCount":320,"candidatesTokenCount":1500,
	"cachedContentTokenCount":100,"totalTokenCount":1920}}`
	ev := parse(t, model.ProviderGemini, raw)
	if ev == nil {
		t.Fatal("沒有解析出事件")
	}
	if ev.Usage.Input != 320 || ev.Usage.Output != 1500 || ev.Usage.CacheRead != 100 {
		t.Errorf("Gemini 欄位對應錯誤: %+v", ev.Usage)
	}
	if ev.Model != "gemini-2.5-pro" {
		t.Errorf("model = %q", ev.Model)
	}
}

// 沒有時間欄位時退回檔案 mtime，並標記為近似值。
func TestFallbackTime(t *testing.T) {
	ev := parse(t, model.ProviderClaude, `{"usage":{"input_tokens":10,"output_tokens":20}}`)
	if ev == nil {
		t.Fatal("沒有解析出事件")
	}
	if !ev.ApproxTime || !ev.At.Equal(fallback) {
		t.Errorf("應退回 mtime 並標記近似: approx=%v at=%v", ev.ApproxTime, ev.At)
	}
}

// 非用量記錄不該產生事件。
func TestIgnoresNonUsage(t *testing.T) {
	for _, raw := range []string{
		`{"type":"user","message":{"role":"user","content":"幫我改這個 bug"}}`,
		`{"type":"summary","summary":"重構完成"}`,
		`{"level":"info","msg":"starting"}`,
	} {
		if ev := parse(t, model.ProviderClaude, raw); ev != nil {
			t.Errorf("不該產生事件: %s -> %+v", raw, ev.Usage)
		}
	}
}

// 若紀錄裡真有官方回報的額度，要能認出來當分母。
func TestReportedQuota(t *testing.T) {
	raw := `{"timestamp":"2026-09-11T09:00:00Z","rate_limits":{
	  "primary":{"used_percent":42.5,"window_seconds":18000,"resets_at":1789003600}}}`
	_, rep, _ := ParseRecord(model.ProviderClaude, []byte(raw), fallback, false)
	if rep == nil {
		t.Fatal("沒有抓到額度資訊")
	}
	if !rep.Usable() {
		t.Fatalf("應可用於進度條: %+v", rep)
	}
	if rep.PercentUsed == nil || *rep.PercentUsed != 42.5 {
		t.Errorf("百分比錯誤: %+v", rep.PercentUsed)
	}
	if rep.ResetAt == nil {
		t.Error("應解析出重置時間")
	}
}

// 同一筆訊息出現在兩個檔案（resume）時只能算一次。
func TestDedupeKeyStable(t *testing.T) {
	raw := `{"uuid":"u-1","message":{"id":"msg_9","usage":{"input_tokens":5,"output_tokens":5}}}`
	a := parse(t, model.ProviderClaude, raw)
	b := parse(t, model.ProviderClaude, raw)
	if a.Key != b.Key {
		t.Errorf("去重鍵不穩定: %q vs %q", a.Key, b.Key)
	}
}

// 沒有任何 id 時退回內容雜湊，內容不同就是不同事件。
func TestHashFallbackKey(t *testing.T) {
	a := parse(t, model.ProviderGemini, `{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2}}`)
	b := parse(t, model.ProviderGemini, `{"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4}}`)
	if a.Key == b.Key {
		t.Error("不同內容不該有相同的雜湊鍵")
	}
}

func TestBlockedFiles(t *testing.T) {
	for _, n := range []string{"oauth_creds.json", "settings.json", "tokens.json", "mcp.json"} {
		if !blockedFile(n) {
			t.Errorf("%s 應被排除", n)
		}
	}
	for _, n := range []string{"rollout-2026-09-11.jsonl", "abc123.jsonl", "logs.json"} {
		if blockedFile(n) {
			t.Errorf("%s 不該被排除", n)
		}
	}
}

// Codex 會同時回報 5 小時與 7 天兩個視窗，欄位名稱一模一樣。
// 分組前這裡會隨 map 迭代順序在 93% 與 46% 之間亂跳，所以跑很多次才有意義。
func TestCodexDualRateLimitWindows(t *testing.T) {
	raw := `{"timestamp":"2026-09-10T18:05:50.252Z","type":"event_msg","payload":{
	  "type":"token_count",
	  "info":{"last_token_usage":{"input_tokens":82019,"output_tokens":81,
	    "cached_input_tokens":80640,"reasoning_output_tokens":0,"total_tokens":82100},
	    "model_context_window":258400},
	  "rate_limits":{"limit_id":"codex","limit_name":null,
	    "primary":{"used_percent":93.0,"window_minutes":300,"resets_at":1789079789},
	    "secondary":{"used_percent":46.0,"window_minutes":10080,"resets_at":1789629957},
	    "credits":{"has_credits":false,"unlimited":false,"balance":"0"}}}}`

	for i := 0; i < 200; i++ {
		_, rep, _ := ParseRecord(model.ProviderCodex, []byte(raw), fallback, false)
		if rep == nil || !rep.Usable() {
			t.Fatalf("第 %d 次沒抓到可用的額度資訊", i)
		}
		if rep.PercentUsed == nil || *rep.PercentUsed != 93 {
			t.Fatalf("第 %d 次主要百分比 = %v，期望 93（5 小時窗）", i, rep.PercentUsed)
		}
		if len(rep.Windows) != 2 {
			t.Fatalf("第 %d 次視窗數 = %d，期望 2", i, len(rep.Windows))
		}
		if rep.Windows[0].Label != "5 小時" || rep.Windows[1].Label != "7 天" {
			t.Fatalf("第 %d 次視窗順序錯誤: %q, %q", i,
				rep.Windows[0].Label, rep.Windows[1].Label)
		}
		if rep.Windows[1].PercentUsed != 46 {
			t.Fatalf("第 %d 次次要百分比 = %v，期望 46", i, rep.Windows[1].PercentUsed)
		}
		// 重置時間必須跟著它自己的視窗走，不能配到另一個窗的。
		if rep.ResetAt == nil || rep.ResetAt.Unix() != 1789079789 {
			t.Fatalf("第 %d 次重置時間 = %v，期望 5 小時窗的 1789079789", i, rep.ResetAt)
		}
		if rep.Windows[1].ResetAt == nil || rep.Windows[1].ResetAt.Unix() != 1789629957 {
			t.Fatalf("第 %d 次週窗重置時間錯誤: %v", i, rep.Windows[1].ResetAt)
		}
	}
}

// 官方回報了重置時間時，rolling 來源要用官方的，而不是自己推的「最舊紀錄滑出」。
