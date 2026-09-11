package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCursorAccounting(t *testing.T) {
	for _, tc := range []struct {
		name, body           string
		used, limit, percent float64
		windows              int
	}{
		{"protobuf omitted zero", `{"billingCycleEnd":"1790410300000","planUsage":{"limit":2000,"autoPercentUsed":0,"apiPercentUsed":0,"totalPercentUsed":0},"spendLimitUsage":{"pooledUsed":8475,"limitType":"team"}}`, 0, 20, 0, 3},
		{"bonus and pooled excluded", `{"planUsage":{"totalSpend":2600,"includedSpend":1000,"bonusSpend":1600,"limit":2000},"spendLimitUsage":{"individualUsed":500,"pooledUsed":8475}}`, 10, 20, 50, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseCursorUsage([]byte(tc.body), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if r.Metric != "usd" || *r.Used != tc.used || *r.Limit != tc.limit || *r.PercentUsed != tc.percent || len(r.Windows) != tc.windows {
				t.Fatalf("unexpected accounting: %+v", r)
			}
			if tc.windows == 3 && (r.ResetAt == nil || r.ResetAt.UnixMilli() != 1790410300000) {
				t.Fatal("billing reset milliseconds lost")
			}
		})
	}
	if _, err := parseCursorUsage([]byte(`{"spendLimitUsage":{"pooledUsed":8475}}`), time.Now()); err == nil {
		t.Fatal("team spending must not become personal quota")
	}
	r, err := parseCursorUsage([]byte(`{"planUsage":{"totalPercentUsed":0}}`), time.Now())
	if err != nil || r.PercentUsed == nil || *r.PercentUsed != 0 || r.Limit != nil {
		t.Fatalf("zero percent without amount: %+v %v", r, err)
	}
	if _, err := parseCursorUsage([]byte(`{"planUsage":{}}`), time.Now()); err == nil {
		t.Fatal("missing is not zero")
	}
}

type cursorTestTransport func(*http.Request) (*http.Response, error)

func (f cursorTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCursorAuthRequestAndErrors(t *testing.T) {
	client := &http.Client{Transport: cursorTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != cursorUsageURL || req.Method != "POST" || req.Header.Get("Authorization") != "Bearer fake-token" {
			t.Fatal("wrong usage request")
		}
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("fake-token sensitive error body")), Header: make(http.Header)}, nil
	})}
	_, err := requestCursorUsage(context.Background(), client, "fake-token", "Cursor CLI")
	if err == nil || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("must not expose auth or server error body: %v", err)
	}
}
