package desktop

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aiusage/internal/config"
	"aiusage/internal/model"
)

var installationCache struct {
	sync.Mutex
	at    time.Time
	found map[string]bool
}

// Check executable/install registrations, not leftover conversation directories.
// A failed inventory is unknown (visible), never evidence of an uninstall.
func InstalledProviders() map[string]bool {
	installationCache.Lock()
	defer installationCache.Unlock()
	if time.Since(installationCache.at) < time.Minute {
		return installationCache.found
	}
	found := map[string]bool{}
	commands := map[string][]string{
		model.ProviderClaude: {"claude.exe", "claude.cmd"}, model.ProviderCodex: {"codex.exe", "codex.cmd"},
		model.ProviderCursor: {"cursor.exe", "cursor.cmd", "cursor-agent.exe"}, model.ProviderAntigravity: {"agy.exe", "antigravity.exe", "antigravity.cmd"},
	}
	for id, names := range commands {
		for _, name := range names {
			if _, err := exec.LookPath(name); err == nil {
				found[id] = true
			}
		}
	}
	local, home := os.Getenv("LOCALAPPDATA"), config.UserHome()
	paths := map[string][]string{
		model.ProviderClaude:      {filepath.Join(home, ".local", "bin", "claude.exe"), filepath.Join(local, "AnthropicClaude", "claude.exe"), filepath.Join(local, "Programs", "Claude", "Claude.exe")},
		model.ProviderCodex:       {filepath.Join(os.Getenv("APPDATA"), "npm", "codex.cmd")},
		model.ProviderCursor:      {filepath.Join(local, "Programs", "cursor", "Cursor.exe"), filepath.Join(home, ".local", "bin", "cursor-agent.exe")},
		model.ProviderAntigravity: {filepath.Join(local, "agy", "bin", "agy.exe"), filepath.Join(local, "Programs", "Antigravity", "Antigravity.exe")},
	}
	for id, candidates := range paths {
		for _, path := range candidates {
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				found[id] = true
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	script := `$ErrorActionPreference='Stop'; $names=@(Get-AppxPackage | Select-Object -ExpandProperty Name); foreach($key in @('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*')) { $names+=@(Get-ItemProperty $key -ErrorAction SilentlyContinue | Select-Object -ExpandProperty DisplayName -ErrorAction SilentlyContinue) }; $names+=@(Get-Process -Name agy,Antigravity,Cursor,Claude,Codex -ErrorAction SilentlyContinue | Select-Object -ExpandProperty ProcessName); ConvertTo-Json -InputObject @($names) -Compress`
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	ConfigureBackgroundProcess(cmd)
	data, err := cmd.Output()
	var names []string
	if err != nil || json.Unmarshal(data, &names) != nil {
		for id := range commands {
			found[id] = true
		}
	} else {
		for _, name := range names {
			name = strings.ToLower(name)
			for id, word := range map[string]string{model.ProviderClaude: "claude", model.ProviderCodex: "codex", model.ProviderCursor: "cursor", model.ProviderAntigravity: "antigravity"} {
				if strings.Contains(name, word) {
					found[id] = true
				}
			}
			if name == "agy" {
				found[model.ProviderAntigravity] = true
			}
		}
	}
	installationCache.at, installationCache.found = time.Now(), found
	return found
}
