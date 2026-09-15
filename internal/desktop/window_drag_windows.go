package desktop

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// 只搬 LockPanelWindows 已認定為面板的視窗，不能因為游標剛好在上面就搬。
var knownPanels struct {
	sync.Mutex
	hwnds []uintptr
}

func publishPanelWindows(hwnds []uintptr) {
	knownPanels.Lock()
	knownPanels.hwnds = append(knownPanels.hwnds[:0], hwnds...)
	knownPanels.Unlock()
}

func panelHandles() []uintptr {
	knownPanels.Lock()
	defer knownPanels.Unlock()
	return append([]uintptr(nil), knownPanels.hwnds...)
}

var dragging atomic.Bool

// BeginPanelDrag 追著游標搬動面板，直到放開左鍵。頁面只在按下時通知一次，
// 每個 mousemove 打一次請求跟不上手感。
func BeginPanelDrag() {
	if len(panelHandles()) == 0 || !dragging.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer dragging.Store(false)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		user := syscall.NewLazyDLL("user32.dll")
		getCursor := user.NewProc("GetCursorPos")
		getRect := user.NewProc("GetWindowRect")
		setPos := user.NewProc("SetWindowPos")
		keyState := user.NewProc("GetAsyncKeyState")

		startX, startY, ok := cursorPosition(getCursor)
		if !ok {
			return
		}
		hwnd, rect, found := panelAt(getRect, startX, startY)
		if !found {
			return
		}
		deadline := time.Now().Add(2 * time.Minute) // 卡住的拖曳不能永遠跟著游標
		for time.Now().Before(deadline) {
			if down, _, _ := keyState.Call(0x01); down&0x8000 == 0 { // VK_LBUTTON
				return
			}
			x, y, ok := cursorPosition(getCursor)
			if !ok {
				return
			}
			left, top := rect.Left+(x-startX), rect.Top+(y-startY)
			setPos.Call(hwnd, ^uintptr(0), uintptr(left), uintptr(top), 0, 0, 0x0001|0x0010) // SWP_NOSIZE|SWP_NOACTIVATE
			time.Sleep(8 * time.Millisecond)
		}
	}()
}

func cursorPosition(getCursor *syscall.LazyProc) (x, y int32, ok bool) {
	var point struct{ X, Y int32 }
	result, _, _ := getCursor.Call(uintptr(unsafe.Pointer(&point)))
	return point.X, point.Y, result != 0
}

func panelAt(getRect *syscall.LazyProc, x, y int32) (uintptr, panelRect, bool) {
	for _, hwnd := range panelHandles() {
		var rect panelRect
		getRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		if cursorOverPanel(x, y, rect.Left, rect.Top, rect.Right, rect.Bottom) {
			return hwnd, rect, true
		}
	}
	return 0, panelRect{}, false
}
