package desktop

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"aiusage/internal/config"
)

// Only the dedicated panel browser is eligible. Never identify by title alone.
func panelBrowserPIDs() map[uint32]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := `$target=[regex]::Escape($env:AIUSAGE_PANEL_PROFILE); $pattern='--user-data-dir=(?:"'+$target+'"|'+$target+'(?=\s|$))'; $ids=@(Get-CimInstance Win32_Process -Filter "Name='msedge.exe' OR Name='chrome.exe'" -ErrorAction Stop | Where-Object { $_.CommandLine -and $_.CommandLine -match $pattern -and $_.CommandLine.Contains('--app=') } | Select-Object -ExpandProperty ProcessId); ConvertTo-Json -InputObject @($ids) -Compress`
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "AIUSAGE_PANEL_PROFILE="+filepath.Join(config.ConfigDir(), "panel-browser"))
	ConfigureBackgroundProcess(cmd)
	data, err := cmd.Output()
	if err != nil {
		return nil
	}
	var ids []uint32
	if json.Unmarshal(data, &ids) != nil {
		return nil
	}
	out := map[uint32]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func LockPanelWindows() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user := syscall.NewLazyDLL("user32.dll")
	user.NewProc("SetThreadDpiAwarenessContext").Call(^uintptr(3)) // Per-monitor v2.
	enum := user.NewProc("EnumWindows")
	pidProc := user.NewProc("GetWindowThreadProcessId")
	titleProc := user.NewProc("GetWindowTextW")
	getStyle := user.NewProc("GetWindowLongPtrW")
	setStyle := user.NewProc("SetWindowLongPtrW")
	setPos := user.NewProc("SetWindowPos")
	getRect := user.NewProc("GetWindowRect")
	getDPI := user.NewProc("GetDpiForWindow")
	isIconic := user.NewProc("IsIconic")
	isZoomed := user.NewProc("IsZoomed")
	show := user.NewProc("ShowWindow")
	menuProc := user.NewProc("GetSystemMenu")
	enableMenu := user.NewProc("EnableMenuItem")
	var pids map[uint32]bool
	last := time.Time{}
	logged := map[uintptr]bool{}
	height := panelHeight(InstalledProviders())
	callback := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var pid uint32
		pidProc.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if !pids[pid] {
			return 1
		}
		var title [256]uint16
		titleProc.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 256)
		if syscall.UTF16ToString(title[:]) != "AI 用量" {
			return 1
		}
		if minimized, _, _ := isIconic.Call(hwnd); minimized != 0 {
			return 1
		}
		if maximized, _, _ := isZoomed.Call(hwnd); maximized != 0 {
			show.Call(hwnd, 9)
		}
		index := ^uintptr(15) // GWL_STYLE = -16
		style, _, _ := getStyle.Call(hwnd, index)
		fixed := style &^ uintptr(0x00040000|0x00010000) // sizing frame / maximize
		if style != fixed {
			setStyle.Call(hwnd, index, fixed)
		}
		menu, _, _ := menuProc.Call(hwnd, 0)
		enableMenu.Call(menu, 0xF000, 1) // SC_SIZE, gray
		enableMenu.Call(menu, 0xF030, 1) // SC_MAXIMIZE, gray
		dpi, _, _ := getDPI.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		width, wheight := int32(360*dpi/96), int32(uintptr(height)*dpi/96)
		var rect struct{ Left, Top, Right, Bottom int32 }
		getRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		if style != fixed || rect.Right-rect.Left != width || rect.Bottom-rect.Top != wheight {
			setPos.Call(hwnd, 0, 0, 0, uintptr(width), uintptr(wheight), 0x0002|0x0004|0x0010|0x0020)
		}
		verified, _, _ := getStyle.Call(hwnd, index)
		if !logged[hwnd] && verified&uintptr(0x50000) == 0 {
			Logf("面板視窗尺寸已鎖定：360 × %d；保留移動、最小化與關閉。", height)
			logged[hwnd] = true
		}
		return 1
	})
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if time.Since(last) > 10*time.Second {
			pids = panelBrowserPIDs()
			last = time.Now()
		}
		enum.Call(callback, 0)
		<-tick.C
	}
}
