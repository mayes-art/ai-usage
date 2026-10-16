package gemini

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
	"strings"
)

func decryptGeminiCookie(key, blob []byte, host string, version int) (string, error) {
	if !bytes.HasPrefix(blob, []byte("v10")) || len(blob) < 3+12+16 {
		return "", fmt.Errorf("unsupported cookie encryption")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, blob[3:15], blob[15:], nil)
	if err != nil {
		return "", err
	}
	// Chromium schema 24 binds each encrypted cookie to its host.
	if version >= 24 {
		digest := sha256.Sum256([]byte(host))
		if !bytes.HasPrefix(plain, digest[:]) {
			return "", fmt.Errorf("cookie host mismatch")
		}
		plain = plain[sha256.Size:]
	}
	value := string(plain)
	for _, b := range plain {
		if b < 0x21 || b > 0x7e || strings.ContainsRune("\";,\\", rune(b)) {
			return "", fmt.Errorf("invalid cookie value")
		}
	}
	return value, nil
}
