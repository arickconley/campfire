package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyMarkdownEscapesHTML(t *testing.T) {
	html, err := LegacyMarkdown("Hello <script>alert(1)</script>\n\n**bold**\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "<strong>bold</strong>") {
		t.Fatalf("HTML: %s", html)
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
