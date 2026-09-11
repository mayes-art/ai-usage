package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPanelStreamDisconnect(t *testing.T) {
	s := NewServer(NewStore(DefaultConfig()))
	s.windows.done = make(chan struct{})
	h := httptest.NewServer(s.mux)
	defer h.Close()
	resp, err := http.Get(h.URL + "/api/stream?t=" + s.token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close() // Same connection cancellation as closing the panel window.
	select {
	case <-s.windows.done:
	case <-time.After(12 * time.Second):
		t.Fatal("closing the actual stream did not request process exit")
	}
}

func TestPanelLastWindowCloses(t *testing.T) {
	p := &panelWindows{done: make(chan struct{})}
	p.attach()
	p.attach()
	p.detach(time.Millisecond)
	select {
	case <-p.done:
		t.Fatal("another window is still open")
	case <-time.After(20 * time.Millisecond):
	}
	p.detach(time.Millisecond)
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("background process did not receive exit")
	}
}

func TestPanelRefreshAndMinimize(t *testing.T) {
	p := &panelWindows{done: make(chan struct{})}
	p.attach()
	p.detach(30 * time.Millisecond)
	p.attach()
	select {
	case <-p.done:
		t.Fatal("refresh closed panel")
	case <-time.After(60 * time.Millisecond):
	}
	// An attached stream stays alive even if the window is minimized.
	p.detach(time.Millisecond)
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("last disconnect did not close")
	}
}

func TestHeadlessDoesNotExit(t *testing.T) {
	p := &panelWindows{}
	p.attach()
	p.detach(time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.timer != nil {
		t.Fatal("headless service must keep running")
	}
}
