package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPServerTimeouts(t *testing.T) {
	srv := httpServer(":8080", http.NewServeMux())
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second || srv.WriteTimeout != 60*time.Second || srv.IdleTimeout != 2*time.Minute {
		t.Fatalf("timeouts header %s read %s write %s idle %s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

func TestServeListenerDrains(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hold", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httpServer(ln.Addr().String(), mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveListener(ln, srv, ctx) }()

	code := make(chan int, 1)
	go func() {
		res, err := http.Get("http://" + ln.Addr().String() + "/hold")
		if err != nil {
			code <- 0
			return
		}
		res.Body.Close()
		code <- res.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before the handler finished: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case got := <-code:
		if got != http.StatusNoContent {
			t.Fatalf("status %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no response")
	}
}

func TestDefaultContent(t *testing.T) {
	got, err := defaultContent()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := filepath.Join(filepath.Dir(exe), "content")
	if got != want {
		t.Fatalf("content %s, want %s", got, want)
	}
}

func TestContentRootEnv(t *testing.T) {
	t.Setenv("CAMPFIRE_CONTENT", "/tmp/campfire-content")
	got, err := contentRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/campfire-content" {
		t.Fatalf("content %s", got)
	}
}
