package desktop

import (
	"slices"
	"testing"
)

func TestPanelBrowserIsolated(t *testing.T) {
	profile := `C:\Users\Test User\AppData\Roaming\aiusage\panel-browser`
	args := panelBrowserArgs("http://127.0.0.1:1234/?t=test", profile, 466)
	for _, want := range []string{"--user-data-dir=" + profile, "--app=http://127.0.0.1:1234/?t=test", "--window-size=360,466", "--disable-background-mode", "--no-first-run"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing isolated window argument %q", want)
		}
	}
}

func TestPanelHeightIncludesVisibleCursorPools(t *testing.T) {
	without := panelHeight(map[string]bool{"codex": true, "claude": true, "cursor": false})
	with := panelHeight(map[string]bool{"codex": true, "claude": true, "cursor": true})
	if with-without != 80+112 {
		t.Fatalf("Cursor card and pools need extra height: %d -> %d", without, with)
	}
	if panelHeight(nil) != 230 {
		t.Fatal("minimum panel height changed")
	}
}
