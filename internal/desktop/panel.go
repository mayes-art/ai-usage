package desktop

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"aiusage/internal/config"
)

// OpenBrowser 在 Windows 上優先用 Edge 的 app 模式開一個沒有網址列的視窗，
// 這樣看起來就是個獨立小工具，而不是一個瀏覽器分頁。
func OpenBrowser(url string) {
	if runtime.GOOS == "darwin" && openMacPanel(url) {
		return
	}
	if runtime.GOOS == "windows" {
		for _, exe := range appModeBrowsers() {
			if _, err := os.Stat(exe); err != nil {
				continue
			}
			count := 0
			for _, installed := range InstalledProviders() {
				if installed {
					count++
				}
			}
			height := 146 + 80*count
			if height < 230 {
				height = 230
			}
			// A dedicated profile isolates app geometry, cookies and browser
			// processes from the user's ordinary Edge/Chrome session.
			profile := filepath.Join(config.ConfigDir(), "panel-browser")
			if err := os.MkdirAll(profile, 0o700); err != nil {
				Logf("無法建立面板專用視窗設定：%v", err)
				return
			}
			cmd := exec.Command(exe, panelBrowserArgs(url, profile, height)...)
			if err := cmd.Start(); err == nil {
				go func() { _ = cmd.Wait() }()
				return
			}
		}
		if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err == nil {
			return
		}
		Logf("（無法自動開啟視窗，請手動貼上網址）")
		return
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", url)
	} else {
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		Logf("（無法自動開啟瀏覽器，請手動貼上網址）")
	}
}

func panelBrowserArgs(url, profile string, height int) []string {
	return []string{
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-mode",
		"--app=" + url,
		fmt.Sprintf("--window-size=360,%d", height),
	}
}

func appModeBrowsers() []string {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LOCALAPPDATA")
	return []string{
		pf86 + `\Microsoft\Edge\Application\msedge.exe`,
		pf + `\Microsoft\Edge\Application\msedge.exe`,
		pf + `\Google\Chrome\Application\chrome.exe`,
		pf86 + `\Google\Chrome\Application\chrome.exe`,
		local + `\Google\Chrome\Application\chrome.exe`,
	}
}

func instanceFile() string { return filepath.Join(config.ConfigDir(), "panel.url") }

// ExistingInstance 檢查上一個實例是否還活著。
func ExistingInstance() (string, bool) {
	data, err := os.ReadFile(instanceFile())
	if err != nil {
		return "", false
	}
	url := strings.TrimSpace(string(data))
	if url == "" {
		return "", false
	}
	check := strings.Replace(url, "/?t=", "/api/snapshot?t=", 1)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(check)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	return url, true
}

func WriteInstanceURL(url string) {
	_ = os.MkdirAll(config.ConfigDir(), 0o700)
	_ = os.WriteFile(instanceFile(), []byte(url), 0o600)
}

func ClearInstanceURL() { _ = os.Remove(instanceFile()) }

// Logf 同時寫到主控台與設定資料夾下的 panel.log。
// 無主控台的版本（-H windowsgui）靠這個檔案才留下訊息。
func Logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	_ = os.MkdirAll(config.ConfigDir(), 0o700)
	f, err := os.OpenFile(filepath.Join(config.ConfigDir(), "panel.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}
