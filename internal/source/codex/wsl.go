package codex

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"aiusage/internal/model"
	"aiusage/internal/source/wsl"
)

const codexWSLBudget = 15 * time.Second

// Only Linux-native codex is used: /mnt paths are Windows shims for the host account.
// nvm installs need their bin directory on PATH for node, since profiles are not loaded.
const codexWSLScript = `c=
for p in "$HOME/.local/bin/codex" "$(command -v codex 2>/dev/null)" "$HOME"/.nvm/versions/node/*/bin/codex "$HOME/.npm-global/bin/codex"; do
  case "$p" in ""|/mnt/*) continue;; esac
  if [ -x "$p" ]; then c=$p; break; fi
done
[ -n "$c" ] || exit 0
PATH="$(dirname "$c"):$PATH" exec "$c" app-server --stdio 2>/dev/null`

type codexWSLCommand func(ctx context.Context, distro, script string) (*exec.Cmd, error)

func readCodexLive(timeout time.Duration) (*model.Reported, error) {
	r, err := readCodexRateLimits(timeout)
	if err == nil {
		return r, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexWSLBudget)
	defer cancel()
	wr, wslErr := readCodexWSL(ctx, wsl.List, wsl.Command, timeout)
	if wr != nil {
		return wr, nil
	}
	if wslErr != nil {
		return nil, fmt.Errorf("host: %v; %v", err, wslErr)
	}
	return nil, err
}

// readCodexWSL returns (nil, nil) when no running distribution has a Linux codex.
func readCodexWSL(ctx context.Context, list func(context.Context) ([]string, error), command codexWSLCommand, timeout time.Duration) (*model.Reported, error) {
	distros, err := list(ctx)
	if err != nil {
		return nil, errors.New("WSL discovery failed or timed out")
	}
	var lastErr error
	for _, distro := range distros {
		if ctx.Err() != nil {
			return nil, errors.New("WSL Codex query timed out")
		}
		dctx, cancel := context.WithTimeout(ctx, timeout)
		cmd, err := command(dctx, distro, codexWSLScript)
		var r *model.Reported
		if err == nil {
			r, err = codexAppServerRateLimits(dctx, cmd, "WSL "+distro+" codex", timeout)
		}
		cancel()
		if errors.Is(err, errCodexAbsent) {
			continue
		}
		if err != nil {
			lastErr = fmt.Errorf("WSL %s Codex rate limits unavailable; sign in with codex login in that distribution", distro)
			continue
		}
		r.Source = "Codex CLI WSL（" + distro + "）帳號額度"
		return r, nil
	}
	return nil, lastErr
}
