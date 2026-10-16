// Package wsl runs fixed commands in running WSL distributions for source collectors.
package wsl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
)

// MaxDistros bounds how many running distributions a collector probes.
const MaxDistros = 8

type output struct {
	data  []byte
	limit int
}

func (w *output) Write(p []byte) (int, error) {
	if len(p) > w.limit-len(w.data) {
		return 0, errors.New("WSL 輸出超過大小限制")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

// ParseDistros accepts `wsl --list --quiet` output in UTF-16LE or UTF-8 and
// drops container-engine VMs, which never host user CLIs.
func ParseDistros(data []byte) []string {
	if len(data) >= 2 && (bytes.IndexByte(data, 0) >= 0 || (data[0] == 0xff && data[1] == 0xfe)) {
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		data = []byte(string(utf16.Decode(words)))
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		name := strings.TrimSpace(line)
		lower := strings.ToLower(name)
		if name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00/\\\r") || strings.HasPrefix(lower, "docker-desktop") || strings.HasPrefix(lower, "podman-machine") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > MaxDistros {
		out = out[:MaxDistros]
	}
	return out
}
