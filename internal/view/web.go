// Package view presents usage data in the browser and terminal.
package view

import (
	_ "embed"
	"net/http"
)

//go:embed ui.html
var indexHTML []byte

func ServeIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexHTML)
}
