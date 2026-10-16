package claude

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"aiusage/internal/model"
)

const wslQuotaPrefix = "Claude Code WSL（"

type wslOutput struct {
	data  []byte
	limit int
}

func (w *wslOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-len(w.data) {
		return 0, errors.New("WSL 輸出超過大小限制")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func readClaudeEnvironments(ctx context.Context, client *http.Client, host func() (string, error), list func(context.Context) ([]string, error), read func(context.Context, string) ([]byte, error)) (*model.Reported, error) {
	token, hostErr := host()
	if hostErr == nil {
		r, err := requestClaudeUsage(ctx, client, token, time.Now)
		if err == nil {
			r.Source = "Claude Code 主機登入帳號額度"
			return r, nil
		}
		var auth claudeAuthError
		if !errors.As(err, &auth) {
			return nil, err
		}
		hostErr = err
	}
	distros, err := list(ctx)
	if err != nil {
		return nil, errors.New("主機 Claude 登入不可用，WSL 探索失敗或逾時；請確認 WSL 可正常執行")
	}
	var lastErr error
	for _, distro := range distros {
		if ctx.Err() != nil {
			return nil, errors.New("Claude 主機／WSL 額度查詢逾時")
		}
		data, err := read(ctx, distro)
		if err != nil {
			lastErr = errors.New("WSL " + distro + " 登入資料無法讀取，請確認該發行版的預設使用者")
			continue
		}
		token, err := parseClaudeAccessToken(data, time.Now())
		if err != nil {
			lastErr = claudeAuthError("WSL " + distro + " 尚無有效 Claude 登入；請在該環境執行 claude auth login")
			continue
		}
		r, err := requestClaudeUsage(ctx, client, token, time.Now)
		if err == nil {
			r.Source = wslQuotaPrefix + distro + "）帳號額度"
			return r, nil
		}
		var auth claudeAuthError
		if !errors.As(err, &auth) {
			return nil, err
		}
		lastErr = claudeAuthError("WSL " + distro + " Claude 登入無效或無查詢權限，請在該環境重新登入")
	}
	if lastErr != nil {
		return nil, fmt.Errorf("主機 Claude 登入不可用；%w", lastErr)
	}
	return nil, hostErr
}

func parseWSLDistros(data []byte) []string {
	if len(data) >= 2 && (bytes.IndexByte(data, 0) >= 0 || (data[0] == 0xff && data[1] == 0xfe)) {
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		data = []byte(string(utf16.Decode(words)))
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00/\\\r") || strings.HasPrefix(strings.ToLower(name), "docker-desktop") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
