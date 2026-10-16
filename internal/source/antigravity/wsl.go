package antigravity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"aiusage/internal/model"
)

const (
	agyWSLBudget  = 40 * time.Second
	agyWSLTimeout = 30 * time.Second
)

// Only Linux-native agy is used: /mnt paths are Windows shims that would report the host account again.
// A missing agy prints nothing and exits 0, so it is skipped rather than reported as an error.
const agyWSLScript = `a=
for p in "$HOME/.local/bin/agy" "$(command -v agy 2>/dev/null)"; do
  case "$p" in ""|/mnt/*) continue;; esac
  if [ -x "$p" ]; then a=$p; break; fi
done
[ -n "$a" ] || exit 0
d=$(mktemp -d) || exit 4
cd "$d" || exit 4
BROWSER="$d/no-browser" "$a" -p /usage --output-format json --print-timeout 25s 2>/dev/null | head -c 1048577
cd /
rm -rf "$d"`

type wslLister func(context.Context) ([]string, error)
type wslExecutor func(ctx context.Context, distro string, limit int, script string) ([]byte, error)

// readAgyWSL returns (nil, nil) when no running distribution has a Linux agy.
func readAgyWSL(ctx context.Context, list wslLister, run wslExecutor) (*model.Reported, error) {
	distros, err := list(ctx)
	if err != nil {
		return nil, errors.New("WSL 探索失敗或逾時")
	}
	var lastErr error
	for _, distro := range distros {
		if ctx.Err() != nil {
			return nil, errors.New("WSL Antigravity 查詢逾時")
		}
		dctx, cancel := context.WithTimeout(ctx, agyWSLTimeout)
		data, err := run(dctx, distro, agyUsageMaxBytes, agyWSLScript)
		cancel()
		if err == nil && len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		var r *model.Reported
		if err == nil {
			r, err = parseAgyUsage(data, time.Now())
		}
		if err != nil {
			lastErr = fmt.Errorf("WSL %s Antigravity CLI 用量報告失敗；請在該環境執行 agy 確認已登入", distro)
			continue
		}
		r.Source = "Antigravity CLI WSL（" + distro + "）用量報告"
		return r, nil
	}
	return nil, lastErr
}
