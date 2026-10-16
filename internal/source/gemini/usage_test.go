package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGeminiCookieAllowlist(t *testing.T) {
	if !geminiCookieAllowed(".gemini.google.com", "__Secure-1PSID") || geminiCookieAllowed("evil.example", "SID") || geminiCookieAllowed(".google.com", "session") || geminiCookieAllowed("accounts.google.com", "SID") {
		t.Fatal("cookie allowlist")
	}
}

func TestParseGeminiUsage(t *testing.T) {
	data := geminiTestFrame(`[2,[[null,0.1,1,[[1792137600]]],[null,0.4,2,[["1792713600",0]]],["100",null,3]]]`)
	r, err := parseGeminiUsage(data, time.Now())
	if err != nil || len(r.Windows) != 2 || math.Abs(*r.PercentUsed-40) > 0.001 || r.ResetAt == nil {
		t.Fatalf("gemini usage: %+v %v", r, err)
	}
	if r.Windows[0].Label != "7 天" || r.ResetAt.Unix() != 1792713600 || r.Windows[1].PercentUsed != 10 {
		t.Fatalf("primary percentage and reset must use the same window: %+v", r)
	}
	for _, data := range [][]byte{
		[]byte(`<html>sign in</html>`),
		geminiTestFrame(`[2,[[null,null,1]]]`),
		geminiTestFrame(`[2,[[null,1.1,1],[null,-0.1,2]]]`),
		geminiTestFrame(`[2,[[null,0.3,3]]]`),
		[]byte(`[["wrb.fr","unrelated","[2,[[null,0.5,1]]] "]]`),
	} {
		if _, err := parseGeminiUsage(data, time.Now()); err == nil {
			t.Fatal("missing or invalid quota accepted")
		}
	}
	r, err = parseGeminiUsage(geminiTestFrame(`[2,[[null,0,1]]]`), time.Now())
	if err != nil || r.PercentUsed == nil || *r.PercentUsed != 0 || r.ResetAt != nil {
		t.Fatal("explicit zero should be known; absent reset should remain unknown")
	}
}

func geminiTestFrame(payload string) []byte {
	frame, _ := json.Marshal([]any{[]any{"wrb.fr", geminiUsageRPC, payload, nil, nil, nil, "generic"}})
	return []byte(fmt.Sprintf(")]}'\n\n%d\n%s\n", len(frame), frame))
}

type geminiTestTransport func(*http.Request) (*http.Response, error)

func (f geminiTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGeminiUsageRequestFlow(t *testing.T) {
	calls := 0
	client := geminiHTTPClient()
	client.Transport = geminiTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Scheme != "https" || req.URL.Host != "gemini.google.com" || req.Header.Get("Cookie") != "SID=synthetic" {
			t.Fatal("unexpected credential destination")
		}
		body := `{"SNlM0e":"synthetic-page-token","cfb2h":"synthetic-build","FdrFJe":"synthetic-session"}`
		if calls == 1 {
			if req.Method != "GET" || req.URL.Path != "/usage" {
				t.Fatal("expected usage page bootstrap")
			}
		} else {
			if calls != 2 || req.Method != "POST" || req.URL.Path != "/_/BardChatUi/data/batchexecute" || req.URL.Query().Get("rpcids") != geminiUsageRPC {
				t.Fatal("unexpected RPC request")
			}
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if req.Form.Get("at") != "synthetic-page-token" || req.Form.Get("f.req") != `[[["jSf9Qc","[]",null,"generic"]]]` || req.URL.Query().Get("bl") != "synthetic-build" {
				t.Fatal("incorrect usage-only RPC form")
			}
			body = string(geminiTestFrame(`[2,[[null,0.25,1]]]`))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	r, err := fetchGeminiUsage(context.Background(), client, "SID=synthetic", time.Now())
	if err != nil || calls != 2 || *r.PercentUsed != 25 || r.Source == "" {
		t.Fatalf("usage flow: %+v, %v, calls=%d", r, err, calls)
	}
}

func TestGeminiRefusesRedirects(t *testing.T) {
	client := geminiHTTPClient()
	calls := 0
	client.Transport = geminiTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://accounts.google.com/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	_, err := fetchGeminiUsage(context.Background(), client, "SID=synthetic", time.Now())
	if !errors.Is(err, errGeminiLogin) || calls != 1 {
		t.Fatalf("redirect followed or not identified: %v, calls=%d", err, calls)
	}
}

func TestGeminiResponseErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{200, 401, 429} {
		client := geminiHTTPClient()
		client.Transport = geminiTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private-response-content"))}, nil
		})
		_, err := fetchGeminiUsage(context.Background(), client, "SID=synthetic", time.Now())
		if err == nil || strings.Contains(err.Error(), "private-response-content") || strings.Contains(err.Error(), "synthetic") {
			t.Fatalf("unsanitized error: %v", err)
		}
	}
}
