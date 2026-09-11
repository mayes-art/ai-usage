package main

import (
	"bytes"
	"image/png"
	"net/http/httptest"
	"testing"
)

func TestRobotIcon(t *testing.T) {
	s := NewServer(NewStore(DefaultConfig()))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/robot-icon.png?v=1", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("favicon unavailable")
	}
	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatal("wrong icon dimensions")
	}
}
