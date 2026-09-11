// Package app wires concrete adapters into the usage service and chooses a UI.
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"aiusage/internal/buildinfo"
	"aiusage/internal/config"
	"aiusage/internal/controller"
	"aiusage/internal/desktop"
	"aiusage/internal/model"
	"aiusage/internal/service"
	"aiusage/internal/source/antigravity"
	"aiusage/internal/source/claude"
	"aiusage/internal/source/codex"
	"aiusage/internal/source/cursor"
	"aiusage/internal/view"
)

// Run executes one invocation without changing os.Args or exiting the process.
// Its return value is the exit status used by the command entry point.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	desktop.PreparePlatformEnvironment()
	if len(args) > 0 && args[0] == "claude-statusline" {
		if err := claude.CaptureStatusline(stdin, stdout); err != nil {
			fmt.Fprintln(stderr, "Claude usage capture:", err)
			return 1
		}
		return 0
	}
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("aiusage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", -1, "面板使用的本機埠，0 表示自動挑選")
	interval := flags.Int("interval", 0, "掃描間隔秒數")
	noBrowser := flags.Bool("no-browser", false, "啟動後不要自動開啟瀏覽器")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "不支援的參數：", strings.Join(flags.Args(), " "))
		return 2
	}
	if *port > 65535 || *port < -1 {
		fmt.Fprintln(stderr, "埠號必須介於 0 與 65535。")
		return 2
	}
	switch command {
	case "version":
		fmt.Fprintln(stdout, "aiusage", buildinfo.Version, runtime.GOOS+"/"+runtime.GOARCH)
		return 0
	case "config":
		fmt.Fprintln(stdout, config.ConfigPath())
		return 0
	case "", "panel", "serve", "report", "doctor":
	default:
		fmt.Fprintf(stderr, "未知的指令 %q。可用：panel、report、doctor、config、version\n", command)
		return 2
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintln(stderr, "設定檔讀取有問題，改用預設值:", err)
	}
	if *port >= 0 {
		cfg.Port = *port
	}
	if *interval > 0 {
		cfg.RefreshSeconds = *interval
	}
	if command == "" || command == "panel" || command == "serve" {
		return runPanel(cfg, *noBrowser)
	}
	store := newStore(cfg)
	if command == "doctor" {
		store.SetCollectKeys(true)
		store.ScanOnce()
		view.Doctor(stdout, store.Diagnose(), buildinfo.Version, config.ConfigPath())
	} else {
		store.ScanOnce()
		view.Report(stdout, store.Snapshot())
	}
	return 0
}

func newStore(cfg *model.Config) *service.Store {
	collectors := map[string]service.Collector{
		model.ProviderCodex:       codex.New(),
		model.ProviderClaude:      claude.New(),
		model.ProviderCursor:      cursor.New(),
		model.ProviderAntigravity: antigravity.New(),
	}
	return service.New(cfg, config.FileRepository{}, collectors, desktop.InstalledProviders)
}

func runPanel(cfg *model.Config, noBrowser bool) int {
	if url, ok := desktop.ExistingInstance(); ok {
		desktop.Logf("面板已在執行中，直接開啟：%s", url)
		if !noBrowser {
			desktop.OpenBrowser(url)
		}
		return 0
	}
	store := newStore(cfg)
	desktop.Logf("正在讀取紀錄…")
	store.ScanOnce()
	server := controller.New(store)
	if !noBrowser {
		server.EnableWindowExit()
	}
	url, closeServer, err := server.Serve(cfg.Port)
	if err != nil {
		desktop.Logf("無法啟動面板：%v", err)
		return 1
	}
	defer closeServer()
	desktop.WriteInstanceURL(url)
	defer desktop.ClearInstanceURL()
	desktop.Logf("面板已啟動：%s", url)
	desktop.Logf("設定檔：%s", config.ConfigPath())
	desktop.Logf("按 Ctrl+C 結束。")
	if !noBrowser {
		desktop.OpenBrowser(url)
		go desktop.LockPanelWindows()
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	interval := time.Duration(cfg.RefreshSeconds) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			store.ScanOnce()
		case <-stop:
			desktop.Logf("已結束。")
			return 0
		case <-server.Done():
			desktop.Logf("所有面板視窗已關閉，結束背景程式。")
			return 0
		}
	}
}
