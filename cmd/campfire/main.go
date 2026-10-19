package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"campfire/internal/content"
	"campfire/internal/server"
)

func main() {
	contentFlag := flag.String("content", "", "content directory (env CAMPFIRE_CONTENT, default content next to the binary)")
	addrFlag := flag.String("addr", "", "listen address (env CAMPFIRE_ADDR, default 127.0.0.1:8080)")
	migrate := flag.Bool("migrate", false, "convert Markdown to HTML, preserve originals in .markdown-backup, then exit")
	rebuild := flag.Bool("rebuild", false, "rebuild saved HTML with current templates and site settings, then exit")
	flag.Parse()

	root, err := contentRoot(*contentFlag)
	if err != nil {
		log.Fatalf("content directory: %v", err)
	}
	addr := pick("addr", *addrFlag, os.Getenv("CAMPFIRE_ADDR"), "127.0.0.1:8080")

	info, err := os.Stat(root)
	if err == nil && !info.IsDir() {
		log.Fatalf("content directory %s is not a directory", root)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("content directory: %v", err)
	}
	if err := content.Seed(root); err != nil {
		log.Fatalf("content directory: %v", err)
	}
	if *migrate {
		if err := content.Migrate(root); err != nil {
			log.Fatal(err)
		}
		log.Print("HTML migration complete; originals are in .markdown-backup")
		return
	}
	if *rebuild {
		if err := content.Rebuild(root); err != nil {
			log.Fatal(err)
		}
		log.Print("HTML pages rebuilt")
		return
	}
	srv, err := server.New(root)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("campfire listening on %s", addr)
	if err := serve(httpServer(addr, srv.Handler())); err != nil {
		log.Fatal(err)
	}
}

const shutdownTimeout = 10 * time.Second

// serve listens until SIGINT or SIGTERM, then waits for in-flight requests.
func serve(srv *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return serveListener(ln, srv, ctx)
}

func serveListener(ln net.Listener, srv *http.Server, ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		err := srv.Shutdown(stopCtx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func httpServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func contentRoot(flagVal string) (string, error) {
	if flagSet("content") {
		return flagVal, nil
	}
	if env := os.Getenv("CAMPFIRE_CONTENT"); env != "" {
		return env, nil
	}
	return defaultContent()
}

func defaultContent() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "content"), nil
}

func pick(name, flagVal, envVal, fallback string) string {
	if flagSet(name) {
		return flagVal
	}
	if envVal != "" {
		return envVal
	}
	return fallback
}

func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
