package gemini

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"testing"
)

func TestDecryptGeminiCookie(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{3}, 12)
	seal := func(plain []byte) []byte {
		blob := append([]byte("v10"), nonce...)
		return gcm.Seal(blob, nonce, plain, nil)
	}
	digest := sha256.Sum256([]byte(".google.com"))
	for _, version := range []int{23, 24} {
		plain := []byte("synthetic-cookie")
		if version >= 24 {
			plain = append(digest[:], plain...)
		}
		blob := seal(plain)
		got, err := decryptGeminiCookie(key, blob, ".google.com", version)
		if err != nil || got != "synthetic-cookie" {
			t.Fatalf("version %d: got %q, %v", version, got, err)
		}
		if version >= 24 {
			if _, err := decryptGeminiCookie(key, blob, "gemini.google.com", version); err == nil {
				t.Fatal("accepted cookie from a different host")
			}
		}
	}
	for _, plain := range [][]byte{[]byte("bad\r\nheader"), {0, 1, 2}, []byte("bad;cookie")} {
		if _, err := decryptGeminiCookie(key, seal(plain), ".google.com", 23); err == nil {
			t.Fatal("accepted invalid cookie value")
		}
	}
	if _, err := decryptGeminiCookie(key, seal([]byte("missing-host-digest")), ".google.com", 24); err == nil {
		t.Fatal("accepted version 24 without host digest")
	}
}
