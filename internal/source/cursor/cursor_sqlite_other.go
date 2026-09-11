//go:build !windows

package cursor

import "fmt"

func readCursorIDEToken(path string) (string, error) {
	return "", fmt.Errorf("IDE 登入讀取目前支援 Windows；其他平台請使用 Cursor CLI 登入")
}
