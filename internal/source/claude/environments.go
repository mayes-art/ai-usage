package claude

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"aiusage/internal/model"
	"aiusage/internal/source/wsl"
)

const wslQuotaPrefix = "Claude Code WSL（"

func readClaudeWSL(ctx context.Context, distro string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// 固定腳本、不載入 shell profile；憑證只經由受限 stdout 留在記憶體。
	const script = `p="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/.credentials.json"; test -f "$p" || exit 3; exec head -c 1048577 -- "$p"`
	return wsl.Exec(ctx, distro, claudeCredentialsMax, script)
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
