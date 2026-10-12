package render

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkdownEscapesHTML(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	html, err := r.Markdown("Hello <script>alert(1)</script>\n\n**bold**\n")
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	if strings.Contains(s, "<script>") {
		t.Fatalf("raw script: %s", s)
	}
	if !strings.Contains(s, "&lt;script&gt;") || !strings.Contains(s, "<strong>bold</strong>") {
		t.Fatalf("html: %s", s)
	}
}

func TestDiskCacheBust(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	a, err := r.Cached(root, "posts/a.md", "Hello **a**\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Cached(root, "posts/a.md", "Hello **a**\n")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || !strings.Contains(string(a), "<strong>a</strong>") {
		t.Fatalf("hit %s", a)
	}
	files, _ := filepath.Glob(filepath.Join(root, ".cache", "posts", "*.html"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	c, err := r.Cached(root, "posts/a.md", "Hello **b**\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c), "<strong>a</strong>") || !strings.Contains(string(c), "<strong>b</strong>") {
		t.Fatalf("bust %s", c)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "<strong>a</strong>") {
		t.Fatalf("stale file %s", raw)
	}
	again, _ := filepath.Glob(filepath.Join(root, ".cache", "posts", "*.html"))
	if len(again) != 1 {
		t.Fatalf("files after edit %v", again)
	}
	Drop(root, "posts/a.md")
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("drop %v", err)
	}
	if _, err := r.Cached(root, "", "no\n"); err == nil {
		t.Fatal("empty path accepted")
	}
}

func TestCacheWriteErrorStillServes(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	html, err := r.Cached(root, "posts/a.md", "Hello **a**\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "<strong>a</strong>") {
		t.Fatalf("html %s", html)
	}
}

func TestCacheStampRejectsBareHash(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	src := "Hello **a**\n"
	full, err := cachePath(root, "posts/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(src))
	old := hex.EncodeToString(sum[:]) + "\n<p>old</p>"
	if err := os.WriteFile(full, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	html, err := r.Cached(root, "posts/a.md", src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "old") || !strings.Contains(string(html), "<strong>a</strong>") {
		t.Fatalf("html %s", html)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	line, _, ok := splitStamp(raw)
	if !ok || !strings.Contains(line, " ") {
		t.Fatalf("stamp %q", line)
	}
}

func TestBadTemplateFallback(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "templates")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blog_posts.html"), []byte("{{end}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	err = r.Feed(root, &buf, View{
		SiteTitle: "Dadaches",
		Posts: []Article{{
			Title:     "Hello",
			HTML:      "<p>Hi</p>",
			ShowTitle: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "Hello") || !strings.Contains(s, "<p>Hi</p>") {
		t.Fatalf("fallback %s", s)
	}
}

func TestFeedPage(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	err = r.Feed("", &buf, View{
		SiteTitle: "Dadaches",
		HeadTitle: "Dadaches",
		Blurb:     "Short notes.",
		Posts: []Article{{
			Title:     "Hello",
			Date:      "9 October 2026",
			URL:       "/posts/hello",
			HTML:      "<p>Hi</p>",
			ShowDate:  true,
			ShowTitle: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"Dadaches", "Short notes.", "9 October 2026", "Hello", "<p>Hi</p>", "/feed.xml"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestTemplateOverride(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := root + "/templates"
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{{if .HasLatest}}LATEST:{{.Latest.Title}}{{end}}{{range .More}}|{{.Title}}{{end}}`
	if err := os.WriteFile(dir+"/blog_posts.html", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	err = r.Feed(root, &buf, View{
		SiteTitle: "Dadaches",
		HasLatest: true,
		Latest:    Article{Title: "First", HTML: "<p>Full</p>"},
		More:      []Article{{Title: "Second"}, {Title: "Third"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "LATEST:First") || !strings.Contains(s, "|Second|Third") {
		t.Fatalf("override %s", s)
	}
	if strings.Contains(s, "<p>Full</p>") {
		t.Fatalf("latest body should stay out of this template: %s", s)
	}
}

func TestBackButton(t *testing.T) {
	if got := (View{}).BackButton(); got != "" {
		t.Fatalf("empty %q", got)
	}
	got := (View{Back: `/now/a"b`}).BackButton()
	if string(got) != `<p><a href="/now/a&#34;b">Back</a></p>` {
		t.Fatalf("button %s", got)
	}
}

func TestAtom(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	when := time.Date(2026, 10, 9, 15, 4, 5, 0, time.UTC)
	err = r.Atom(&buf, AtomView{
		Title:   "Dadaches",
		BaseURL: "http://example.test",
		Updated: when,
		Entries: []AtomEntry{{
			Title:   "Hello",
			URL:     "http://example.test/posts/hello",
			Updated: when,
			HTML:    "<p>Hi</p>",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, `http://example.test/posts/hello`) || !strings.Contains(s, "<feed") {
		t.Fatalf("atom: %s", s)
	}
}
