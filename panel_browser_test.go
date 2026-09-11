package main

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
