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

type panelRect struct{ Left, Top, Right, Bottom int32 }

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
	getCursor := user.NewProc("GetCursorPos")
	setLayeredAttributes := user.NewProc("SetLayeredWindowAttributes")
	getLayeredAttributes := user.NewProc("GetLayeredWindowAttributes")
	isIconic := user.NewProc("IsIconic")
	isZoomed := user.NewProc("IsZoomed")
	show := user.NewProc("ShowWindow")
	menuProc := user.NewProc("GetSystemMenu")
	enableMenu := user.NewProc("EnableMenuItem")

	windowRect := func(hwnd uintptr) panelRect {
		var rect panelRect
		getRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		return rect
	}
	wantAlpha := func(hwnd uintptr) byte {
		var point struct{ X, Y int32 }
		if ok, _, _ := getCursor.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
			return panelAlpha
		}
		rect := windowRect(hwnd)
		if cursorOverPanel(point.X, point.Y, rect.Left, rect.Top, rect.Right, rect.Bottom) {
			return panelAlphaHover
		}
		return panelAlpha
	}
	currentAlpha := func(hwnd uintptr) (byte, bool) {
		var alpha byte
		var flags uint32
		result, _, _ := getLayeredAttributes.Call(hwnd, 0, uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
		return alpha, result != 0 && flags&0x00000002 != 0 // LWA_ALPHA
	}
	applyAlpha := func(hwnd uintptr, want byte) {
		if alpha, ok := currentAlpha(hwnd); ok && alpha == want {
			return
		}
		setLayeredAttributes.Call(hwnd, 0, uintptr(want), 0x00000002)
	}

	var pids map[uint32]bool
	var panels []uintptr
	last := time.Time{}
	configured := map[uintptr]bool{}
	failed := map[uintptr]bool{}
	width, height := panelSize(InstalledProviders())
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
		panels = append(panels, hwnd)
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
		scaledWidth, scaledHeight := int32(uintptr(width)*dpi/96), int32(uintptr(height)*dpi/96)
		rect := windowRect(hwnd)
		exIndex := ^uintptr(19) // GWL_EXSTYLE = -20
		exStyle, _, _ := getStyle.Call(hwnd, exIndex)
		layered := exStyle | uintptr(0x00080000) // WS_EX_LAYERED
		if exStyle != layered {
			setStyle.Call(hwnd, exIndex, layered)
		}
		want := wantAlpha(hwnd)
		applyAlpha(hwnd, want)
		topmost := exStyle&uintptr(0x00000008) != 0 // WS_EX_TOPMOST
		if !configured[hwnd] || style != fixed || exStyle != layered || !topmost || rect.Right-rect.Left != scaledWidth || rect.Bottom-rect.Top != scaledHeight {
			setPos.Call(hwnd, ^uintptr(0), 0, 0, uintptr(scaledWidth), uintptr(scaledHeight), 0x0002|0x0010|0x0020)
		}
		verified, _, _ := getStyle.Call(hwnd, index)
		verifiedEx, _, _ := getStyle.Call(hwnd, exIndex)
		rect = windowRect(hwnd)
		alpha, alphaOK := currentAlpha(hwnd)
		ok := verified&uintptr(0x50000) == 0 &&
			verifiedEx&uintptr(0x00080008) == uintptr(0x00080008) &&
			rect.Right-rect.Left == scaledWidth && rect.Bottom-rect.Top == scaledHeight &&
			alphaOK && alpha == want
		if !configured[hwnd] && ok {
			Logf("面板視窗已設為最上層並鎖定：%d × %d；平常 %d%% 不透明，滑鼠移入時完全不透明。保留移動、最小化與關閉。",
				width, height, panelOpacityPercent)
		}
		if !ok && !failed[hwnd] {
			Logf("面板視窗樣式尚未完整套用，將在背景重試。")
			failed[hwnd] = true
		}
		configured[hwnd] = ok
		return 1
	})
	tick := time.NewTicker(125 * time.Millisecond)
	defer tick.Stop()
	full := time.Time{}
	for {
		if time.Since(last) > 10*time.Second {
			pids = panelBrowserPIDs()
			last = time.Now()
		}
		if time.Since(full) >= time.Second {
			panels = panels[:0]
			enum.Call(callback, 0)
			publishPanelWindows(panels)
			full = time.Now()
		} else {
			for _, hwnd := range panels {
				if minimized, _, _ := isIconic.Call(hwnd); minimized != 0 {
					continue
				}
				applyAlpha(hwnd, wantAlpha(hwnd))
			}
		}
		<-tick.C
	}
}
