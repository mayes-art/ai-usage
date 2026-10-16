package claude

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestParseWSLDistros(t *testing.T) {
	input := "Ubuntu\r\ndocker-desktop\r\nDebian\r\nUbuntu\r\n"
	words := utf16.Encode([]rune("\ufeff" + input))
	wide := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(wide[2*i:], word)
	}
	for _, data := range [][]byte{[]byte(input), wide} {
		if got := parseWSLDistros(data); !reflect.DeepEqual(got, []string{"Debian", "Ubuntu"}) {
			t.Fatalf("invalid WSL names: %q", got)
		}
	}
}

func TestClaudeHostAndWSLFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hostMissing bool
		hostStatus  int
		wantWSL     bool
		wantError   bool
	}{
		{"host wins", false, 200, false, false},
		{"expired host uses WSL", true, 200, true, false},
		{"rejected host uses WSL", false, 401, true, false},
		{"forbidden host uses WSL", false, 403, true, false},
		{"rate limit does not switch accounts", false, 429, false, true},
		{"service failure does not switch accounts", false, 503, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed, wslRequests := 0, 0
			client := &http.Client{Transport: claudeTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") == "Bearer synthetic-host" {
					return claudeTestResponse(tc.hostStatus, `{"five_hour":{"utilization":0},"seven_day":{"utilization":30}}`), nil
				}
				if req.Header.Get("Authorization") != "Bearer synthetic-wsl" {
					t.Fatal("unexpected token")
				}
				wslRequests++
				return claudeTestResponse(200, `{"five_hour":{"utilization":12}}`), nil
			})}
			r, err := readClaudeEnvironments(context.Background(), client, func() (string, error) {
				if tc.hostMissing {
					return "", claudeAuthError("synthetic expired")
				}
				return "synthetic-host", nil
			}, func(context.Context) ([]string, error) { listed++; return []string{"Ubuntu"}, nil },
				func(context.Context, string) ([]byte, error) {
					return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-wsl"}}`), nil
				})
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantWSL {
				if listed != 1 || wslRequests != 1 || r == nil || *r.PercentUsed != 12 || !strings.Contains(r.Source, "Ubuntu") {
					t.Fatalf("WSL fallback failed: %+v", r)
				}
			} else if listed != 0 || wslRequests != 0 {
				t.Fatal("queried WSL without host authentication failure")
			}
		})
	}
}

func TestWSLFailuresDoNotExposeProcessOutput(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"synthetic-secret":`), []byte(strings.Repeat("x", claudeCredentialsMax+1))} {
		_, err := readClaudeEnvironments(context.Background(), &http.Client{}, func() (string, error) { return "", claudeAuthError("host expired") },
			func(context.Context) ([]string, error) { return []string{"Ubuntu"}, nil },
			func(context.Context, string) ([]byte, error) { return data, nil })
		if err == nil {
			t.Fatal("invalid credentials accepted")
		}
		assertClaudeErrorSanitized(t, err, "synthetic-secret")
	}
	_, err := readClaudeEnvironments(context.Background(), &http.Client{}, func() (string, error) { return "", claudeAuthError("host expired") },
		func(context.Context) ([]string, error) { return nil, errors.New("synthetic-private-process-error") }, nil)
	assertClaudeErrorSanitized(t, err, "synthetic-private-process-error")
}

func TestWSLOutputBound(t *testing.T) {
	w := &wslOutput{limit: 4}
	if _, err := w.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("sensitive")); err == nil || string(w.data) != "1234" {
		t.Fatal("unbounded output")
	}
}
