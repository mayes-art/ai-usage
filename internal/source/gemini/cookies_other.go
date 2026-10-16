//go:build !windows

package gemini

import "fmt"

func geminiCookieHeader() (string, error) {
	return "", fmt.Errorf("Gemini 桌面版登入讀取目前支援 Windows")
}
