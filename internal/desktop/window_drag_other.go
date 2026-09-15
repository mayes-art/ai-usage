//go:build !windows

package desktop

// 搬動面板視窗只在 Windows 整合；其他平台交給瀏覽器自己的視窗管理。
func BeginPanelDrag() {}
