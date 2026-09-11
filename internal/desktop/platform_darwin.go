package desktop

import (
	"os"
	"os/exec"
	"path/filepath"

	"aiusage/internal/config"
	"aiusage/internal/model"
)

func PreparePlatformEnvironment() {
	// Finder does not inherit interactive-shell PATH (notably Apple Silicon brew).
	paths := []string{os.Getenv("PATH"), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(config.UserHome(), ".local", "bin"), "/usr/bin", "/bin"}
	path := ""
	for _, p := range paths {
		if p != "" {
			if path != "" {
				path += ":"
			}
			path += p
		}
	}
	_ = os.Setenv("PATH", path)
}

func openMacPanel(url string) bool {
	for _, root := range []string{"/Applications", filepath.Join(config.UserHome(), "Applications")} {
		for _, app := range []string{"Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "Google Chrome.app/Contents/MacOS/Google Chrome"} {
			exe := filepath.Join(root, app)
			if _, err := os.Stat(exe); err != nil {
				continue
			}
			profile := filepath.Join(config.ConfigDir(), "panel-browser")
			if os.MkdirAll(profile, 0700) != nil {
				return false
			}
			count := 0
			for _, yes := range InstalledProviders() {
				if yes {
					count++
				}
			}
			height := 146 + 80*count
			if height < 230 {
				height = 230
			}
			cmd := exec.Command(exe, panelBrowserArgs(url, profile, height)...)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
				return true
			}
		}
	}
	return false
}

func InstalledProviders() map[string]bool {
	found := map[string]bool{}
	names := map[string][]string{model.ProviderCodex: {"codex"}, model.ProviderClaude: {"claude"}, model.ProviderCursor: {"cursor", "cursor-agent", "agent"}, model.ProviderAntigravity: {"agy", "antigravity"}}
	for id, commands := range names {
		for _, name := range commands {
			if _, err := exec.LookPath(name); err == nil {
				found[id] = true
			}
		}
	}
	for id, app := range map[string]string{model.ProviderCodex: "Codex.app", model.ProviderClaude: "Claude.app", model.ProviderCursor: "Cursor.app", model.ProviderAntigravity: "Antigravity.app"} {
		for _, root := range []string{"/Applications", filepath.Join(config.UserHome(), "Applications")} {
			if st, err := os.Stat(filepath.Join(root, app)); err == nil && st.IsDir() {
				found[id] = true
			}
		}
	}
	return found
}
