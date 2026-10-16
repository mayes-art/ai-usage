package antigravity

import (
	"aiusage/internal/model"
	"aiusage/internal/record"
	"aiusage/internal/source/wsl"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"
)

type antigravityEndpoint struct {
	Port   int    `json:"port"`
	Source string `json:"source"`
	CSRF   string `json:"csrf"`
}

func parseAntigravityQuota(data []byte, now time.Time) (*model.Reported, error) {
	var doc struct {
		Response struct {
			Groups []struct {
				DisplayName string `json:"displayName"`
				Buckets     []struct {
					BucketID    string   `json:"bucketId"`
					Window      string   `json:"window"`
					DisplayName string   `json:"displayName"`
					Remaining   *float64 `json:"remainingFraction"`
					Reset       string   `json:"resetTime"`
				} `json:"buckets"`
			} `json:"groups"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, fmt.Errorf("Antigravity 額度摘要格式異常")
	}
	var groups []quotaGroup
	for _, g := range doc.Response.Groups {
		group := quotaGroup{Name: g.DisplayName}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, quotaBucket{ID: b.BucketID, Name: b.DisplayName, Window: b.Window, Remaining: b.Remaining, Reset: b.Reset})
		}
		groups = append(groups, group)
	}
	return quotaFromGroups(groups, now)
}

type quotaGroup struct {
	Name    string
	Buckets []quotaBucket
}

type quotaBucket struct {
	ID, Name, Window, Reset string
	Remaining               *float64
}

func quotaFromGroups(groups []quotaGroup, now time.Time) (*model.Reported, error) {
	r := &model.Reported{At: now, Metric: "quota"}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, b := range group.Buckets {
			if b.Remaining == nil || math.IsNaN(*b.Remaining) || math.IsInf(*b.Remaining, 0) || *b.Remaining < 0 || *b.Remaining > 1 {
				continue
			}
			key := group.Name + "|" + b.ID
			if b.ID != "" && seen[key] {
				continue
			}
			seen[key] = true
			label := b.Name
			mins := float64(0)
			switch b.Window {
			case "5h":
				label = "5 小時"
				mins = 300
			case "weekly":
				label = "7 天"
				mins = 10080
			}
			w := model.ReportedWindow{Label: group.Name + " / " + label, WindowMinutes: mins, PercentUsed: (1 - *b.Remaining) * 100, HasPercent: true}
			if t, ok := record.ParseTimeString(b.Reset); ok {
				w.ResetAt = &t
			}
			r.Windows = append(r.Windows, w)
		}
	}
	return finishAntigravityQuota(r)
}

func parseAntigravityModels(data []byte, now time.Time) (*model.Reported, error) {
	var doc struct {
		UserStatus struct {
			Data struct {
				Models []struct {
					Label   string `json:"label"`
					ModelID string `json:"modelId"`
					Quota   *struct {
						Remaining *float64 `json:"remainingFraction"`
						Reset     string   `json:"resetTime"`
					} `json:"quotaInfo"`
				} `json:"clientModelConfigs"`
			} `json:"cascadeModelConfigData"`
		} `json:"userStatus"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, fmt.Errorf("Antigravity 模型額度格式異常")
	}
	r := &model.Reported{At: now, Metric: "quota"}
	for _, m := range doc.UserStatus.Data.Models {
		if m.Quota == nil || m.Quota.Remaining == nil {
			continue
		}
		v := *m.Quota.Remaining
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			continue
		}
		label := m.Label
		if label == "" {
			label = m.ModelID
		}
		w := model.ReportedWindow{Label: label, PercentUsed: (1 - v) * 100, HasPercent: true}
		if t, ok := record.ParseTimeString(m.Quota.Reset); ok {
			w.ResetAt = &t
		}
		r.Windows = append(r.Windows, w)
	}
	return finishAntigravityQuota(r)
}

func finishAntigravityQuota(r *model.Reported) (*model.Reported, error) {
	if len(r.Windows) == 0 {
		return nil, fmt.Errorf("Antigravity 尚未提供額度，請登入並在 CLI 執行 /usage 或開啟 Desktop 用量頁面")
	}
	// The most-used window leads; independent model groups are never added.
	sort.Slice(r.Windows, func(i, j int) bool {
		a, b := r.Windows[i], r.Windows[j]
		if a.PercentUsed != b.PercentUsed {
			return a.PercentUsed > b.PercentUsed
		}
		return a.Label < b.Label
	})
	r.PercentUsed = &r.Windows[0].PercentUsed
	r.ResetAt = r.Windows[0].ResetAt
	return r, nil
}

func antigravityRequest(ctx context.Context, e antigravityEndpoint, method string) ([]byte, int, error) {
	if e.Port < 1 || e.Port > 65535 {
		return nil, 0, fmt.Errorf("invalid local port")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/%s", e.Port, method), bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if e.CSRF != "" {
		req.Header.Set("X-Codeium-Csrf-Token", e.CSRF)
	}
	// Never route local service tokens through an environment HTTP proxy.
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Antigravity 本機服務無法連線或逾時")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("Antigravity 本機服務 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, 200, fmt.Errorf("Antigravity 用量回應無法讀取")
	}
	return data, 200, nil
}

func readAntigravityQuota() (*model.Reported, error) {
	r, err := readAntigravityHost()
	if err == nil {
		return r, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), agyWSLBudget)
	defer cancel()
	wr, wslErr := readAgyWSL(ctx, wsl.List, wsl.Exec)
	if wr != nil {
		return wr, nil
	}
	if wslErr != nil {
		return nil, fmt.Errorf("主機：%v；%v", err, wslErr)
	}
	return nil, err
}

func readAntigravityHost() (*model.Reported, error) {
	r, err := readAntigravityService()
	if err == nil {
		return r, nil
	}
	exe, findErr := findAgyCommand()
	if findErr != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), agyUsageTimeout)
	defer cancel()
	return readAgyUsage(ctx, exe)
}

func readAntigravityService() (*model.Reported, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoints, err := discoverAntigravity(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(endpoints, func(i, j int) bool { return endpoints[i].Port > endpoints[j].Port })
	lastErr := fmt.Errorf("未找到執行中的 Antigravity CLI / Desktop；請開啟並登入")
	for _, e := range endpoints {
		data, status, err := antigravityRequest(ctx, e, "RetrieveUserQuotaSummary")
		var r *model.Reported
		if err == nil {
			r, err = parseAntigravityQuota(data, time.Now())
		}
		if status == 404 || status == 501 || status == 200 && err != nil {
			data, _, err = antigravityRequest(ctx, e, "GetUserStatus")
			if err == nil {
				r, err = parseAntigravityModels(data, time.Now())
			}
		}
		if err == nil {
			r.Source = e.Source + " 本機額度介面"
			return r, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
