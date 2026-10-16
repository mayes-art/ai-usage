package wsl

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestParseDistros(t *testing.T) {
	input := "Ubuntu\r\ndocker-desktop\r\npodman-machine-default\r\nDebian\r\nUbuntu\r\n"
	words := utf16.Encode([]rune("\ufeff" + input))
	wide := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(wide[2*i:], word)
	}
	for _, data := range [][]byte{[]byte(input), wide} {
		if got := ParseDistros(data); !reflect.DeepEqual(got, []string{"Debian", "Ubuntu"}) {
			t.Fatalf("invalid WSL names: %q", got)
		}
	}
}

func TestOutputBound(t *testing.T) {
	w := &output{limit: 4}
	if _, err := w.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("sensitive")); err == nil || string(w.data) != "1234" {
		t.Fatal("unbounded output")
	}
}
