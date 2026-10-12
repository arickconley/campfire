package site

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndExists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blog")
	if Exists(root) {
		t.Fatal("exists before write")
	}
	err := Write(root, Config{Title: "  Hello: there ", Description: " Notes ", URL: "http://localhost:8080/", Style: " ink "})
	if err != nil {
		t.Fatal(err)
	}
	if !Exists(root) {
		t.Fatal("missing after write")
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Hello: there" || cfg.Description != "Notes" || cfg.URL != "http://localhost:8080" || cfg.Style != "ink" {
		t.Fatalf("%+v", cfg)
	}
	if err := Write(root, Config{Title: "Next", Style: "paper"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Next" || cfg.Style != "paper" {
		t.Fatalf("replace %+v", cfg)
	}
	assertNoTemp(t, root)
}

func TestWriteRejects(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, Config{Style: "paper"}); !errors.Is(err, ErrTitle) {
		t.Fatalf("title %v", err)
	}
	if err := Write(root, Config{Title: "Hi", Style: "../x"}); !errors.Is(err, ErrStyle) {
		t.Fatalf("style %v", err)
	}
	if Exists(root) {
		t.Fatal("wrote a rejected file")
	}
}

func TestTokenFile(t *testing.T) {
	root := t.TempDir()
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || a == b {
		t.Fatalf("tokens %q %q", a, b)
	}
	if err := SaveToken(root, a); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != a {
		t.Fatalf("load %q", got)
	}
	info, err := os.Stat(filepath.Join(root, ".token"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	assertNoTemp(t, root)
	missing, err := LoadToken(t.TempDir())
	if err != nil || missing != "" {
		t.Fatalf("missing %q %v", missing, err)
	}
}

func assertNoTemp(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "-") && strings.HasPrefix(e.Name(), ".") && e.Name() != ".token" {
			t.Fatalf("temp left behind %s", e.Name())
		}
	}
}

func TestStylesFallback(t *testing.T) {
	got := Styles(t.TempDir())
	if len(got) != 3 || got[0] != "paper" {
		t.Fatalf("%v", got)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles", "ink.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = Styles(root)
	if len(got) != 1 || got[0] != "ink" {
		t.Fatalf("%v", got)
	}
}
