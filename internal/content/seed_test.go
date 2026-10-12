package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "content")
	if err := Seed(root); err != nil {
		t.Fatal(err)
	}
	assertStarter(t, root)
	if err := Seed(root); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(root, "posts", "hello.md")
	if err := os.WriteFile(hello, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(root); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(hello)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "edited\n" {
		t.Fatalf("overwrote hello %q", b)
	}
}

func TestSeedEmptyAndDotfiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(root); err != nil {
		t.Fatal(err)
	}
	assertStarter(t, root)
	if _, err := os.Stat(filepath.Join(root, ".DS_Store")); err != nil {
		t.Fatal(err)
	}
}

func TestSeedLeavesExisting(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "posts", "mine.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "posts", "hello.md")); !os.IsNotExist(err) {
		t.Fatalf("seeded over existing tree %v", err)
	}
}

func TestSeedRejectsFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "content")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(root); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("%v", err)
	}
	if err := Seed("  "); err == nil {
		t.Fatal("empty root")
	}
}

func assertStarter(t *testing.T, root string) {
	t.Helper()
	for _, rel := range []string{
		"index.md",
		"about.md",
		"posts/hello.md",
		"media/mark.svg",
		"styles/paper.css",
		"styles/ink.css",
		"styles/plain.css",
		"styles/nerd.css",
		"templates/layout.html",
		"templates/blog_posts.html",
		"templates/blog_list.html",
		"templates/blog_post.html",
		"templates/notfound.html",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "site.yaml")); !os.IsNotExist(err) {
		t.Fatalf("site.yaml %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".token")); !os.IsNotExist(err) {
		t.Fatalf(".token %v", err)
	}
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	hello, ok := ix.Find("hello")
	if !ok || hello.Draft || hello.Title != "Hello" || !strings.Contains(hello.Body, "The fire is lit.") {
		t.Fatalf("hello %+v %v", hello, ok)
	}
	if _, ok := ix.Blurb(); !ok {
		t.Fatal("no blurb")
	}
	if _, ok := ix.Page("/about"); !ok {
		t.Fatal("no about")
	}
	if rel, ok := ix.Static("/styles/paper.css"); !ok || rel != "styles/paper.css" {
		t.Fatalf("css %q %v", rel, ok)
	}
	if rel, ok := ix.Static("/media/mark.svg"); !ok || rel != "media/mark.svg" {
		t.Fatalf("mark %q %v", rel, ok)
	}
	if _, ok := ix.Static("/templates/blog_posts.html"); ok {
		t.Fatal("templates served")
	}
}
