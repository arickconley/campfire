package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Site" {
		t.Fatalf("title %q", cfg.Title)
	}
}

func TestLoadFile(t *testing.T) {
	root := t.TempDir()
	body := "title: Dadaches\ndescription: Notes.\nurl: http://example.test/\nstyle: ink\n"
	if err := os.WriteFile(filepath.Join(root, "site.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Dadaches" || cfg.Description != "Notes." || cfg.URL != "http://example.test" || cfg.Style != "ink" {
		t.Fatalf("%+v", cfg)
	}
}

func TestPublicURL(t *testing.T) {
	for _, tc := range []struct{ base, route, want string }{
		{"https://example.test/", "/posts/a", "https://example.test/posts/a"},
		{"https://example.test", "/", "https://example.test/"},
		{"https://example.test/blog/", "/a & b", "https://example.test/blog/a%20&%20b"},
		{"", "/", ""}, {"example.test", "/", ""}, {"javascript:alert(1)", "/", ""},
		{"https://user:pass@example.test", "/", ""}, {"https://example.test/?x=1", "/", ""},
		{"https://example.test/#fragment", "/", ""},
	} {
		if got := (Config{URL: tc.base}).PublicURL(tc.route); got != tc.want {
			t.Errorf("%q + %q = %q, want %q", tc.base, tc.route, got, tc.want)
		}
	}
}
