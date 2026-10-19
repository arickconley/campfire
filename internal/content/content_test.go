package content

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTree(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"index.md":              "---\ntitle: Home\n---\nBlurb text.\n",
		"about.md":              "---\ntitle: About\n---\nAbout text.\n",
		"now/index.md":          "---\ntitle: Now\n---\nNow text.\n",
		"now/week.md":           "week text\n",
		"posts/old.md":          "---\ntitle: Old\ndate: 2020-05-05T00:00:00Z\n---\nOLDPOST\n",
		"posts/new.md":          "---\ntitle: New\ndate: 2026-06-01T00:00:00Z\n---\nNEWPOST\n",
		"posts/secret.md":       "---\ndraft: true\n---\nDRAFTSECRET\n",
		"posts/nested/nope.md":  "NESTEDPAGE\n",
		"gone.md":               "---\ntitle: Gone\ndraft: true\n---\nGONE\n",
		"_hidden.md":            "HIDDENSECRET\n",
		".secret.md":            "DOTSECRET\n",
		".cache/posts/old.html": "CACHEDSECRET\n",
		"media/mark.svg":        "<svg></svg>\n",
		"site.yaml":             "title: Dadaches\n",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(root); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	blurb, ok := ix.Blurb()
	if !ok || !strings.Contains(blurb.Body, "Blurb text.") {
		t.Fatalf("blurb: %+v %v", blurb, ok)
	}
	if _, ok := ix.Page("/"); ok {
		t.Fatal("root index should not be its own page")
	}

	about, ok := ix.Page("/about")
	if !ok || about.Title != "About" {
		t.Fatalf("about: %+v", about)
	}
	now, ok := ix.Page("/now")
	if !ok || now.Title != "Now" {
		t.Fatalf("now: %+v", now)
	}
	if _, ok := ix.Page("/now/week"); !ok {
		t.Fatal("missing /now/week")
	}
	if _, ok := ix.Page("/posts/nested/nope"); !ok {
		t.Fatal("missing nested page")
	}
	if rel, ok := ix.Static("/media/mark.svg"); !ok || rel != "media/mark.svg" {
		t.Fatalf("static: %q %v", rel, ok)
	}
	if _, ok := ix.Static("/site.yaml"); ok {
		t.Fatal("site.yaml was served")
	}
	if _, ok := ix.Static("/.cache/posts/old.html"); ok {
		t.Fatal("cache was served")
	}
	for _, d := range ix.Docs {
		if strings.Contains(d.Body, "CACHEDSECRET") || strings.Contains(d.Body, "DOTSECRET") {
			t.Fatalf("hidden %s", d.Rel)
		}
	}

	pages := ix.Pages()
	got := make([]string, len(pages))
	for i, d := range pages {
		got[i] = d.URL
		if d.Draft || d.Kind != KindPage {
			t.Fatalf("page %+v", d)
		}
	}
	want := []string{"/about", "/now", "/now/week", "/posts/nested/nope"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pages %v", got)
	}

	feed := ix.Feed()
	if len(feed) != 2 || feed[0].Slug != "new" || feed[1].Slug != "old" {
		t.Fatalf("feed: %+v", feed)
	}
	if !feed[1].Date.Equal(time.Date(2020, 5, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("document date should win: %s", feed[1].Date)
	}
	if !feed[0].Date.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("document date: %s", feed[0].Date)
	}
	for _, d := range feed {
		if strings.Contains(d.Body, "DRAFT") || strings.Contains(d.Body, "NESTED") || strings.Contains(d.Body, "HIDDEN") {
			t.Fatalf("feed leaked %s", d.Body)
		}
	}
	secret, ok := ix.Find("secret")
	if !ok || !secret.Draft {
		t.Fatal("draft missing from api index")
	}
}

func TestWrites(t *testing.T) {
	root := t.TempDir()
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	doc, err := WritePost(root, Input{Title: "Hello World", Date: when, Body: "Hi there"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Slug != "hello-world" {
		t.Fatalf("slug %s", doc.Slug)
	}
	full := filepath.Join(root, "posts", "hello-world.html")
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `name="campfire-date" content="2026-03-04T05:06:07Z"`) {
		t.Fatalf("date not in head metadata:\n%s", raw)
	}
	_, err = WritePost(root, Input{Slug: "hello-world", Body: "again"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("exists: %v", err)
	}
	_, err = WritePost(root, Input{Slug: "../x", Body: "no"})
	if !errors.Is(err, ErrSlug) {
		t.Fatalf("slug: %v", err)
	}
	_, err = WritePost(root, Input{Slug: "ok", Body: "  "})
	if !errors.Is(err, ErrBody) {
		t.Fatalf("body: %v", err)
	}

	updated, err := UpdatePost(root, "hello-world", Input{
		Title: "Edited",
		Body:  "changed",
		Date:  time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Edited" || !strings.Contains(updated.Body, "changed") {
		t.Fatalf("update %+v", updated)
	}
	raw, err = os.ReadFile(full)
	if err != nil {
		t.Fatal("filename changed")
	}
	if !strings.Contains(string(raw), `name="campfire-date" content="2026-08-09T00:00:00Z"`) {
		t.Fatalf("updated date:\n%s", raw)
	}
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ix.Find("hello-world")
	if !ok || got.Title != "Edited" || !strings.Contains(got.Body, "changed") {
		t.Fatalf("reloaded %+v", got)
	}

	if _, err := DeletePost(root, "hello-world"); err != nil {
		t.Fatal(err)
	}
	if _, err := DeletePost(root, "hello-world"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSkipBadHTML(t *testing.T) {
	root := t.TempDir()
	if _, err := WritePost(root, Input{Slug: "ok", Body: "<p>hello</p>"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "posts/bad.html"), []byte(`<html><head><meta name="campfire-draft" content="invalid"/></head><body>SECRET</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Find("ok"); !ok {
		t.Fatal("missing valid post")
	}
	if _, ok := ix.Find("bad"); ok {
		t.Fatal("kept invalid HTML")
	}
	if _, ok := ix.Static("/posts/bad.html"); ok {
		t.Fatal("exposed invalid HTML")
	}
}

func TestSymlinkRoot(t *testing.T) {
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "about.md"), []byte("about text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "content")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(link); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(link)
	if err != nil {
		t.Fatal(err)
	}
	about, ok := ix.Page("/about")
	if !ok || !strings.Contains(about.Body, "about text") {
		t.Fatalf("about %+v %v", about, ok)
	}

	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(real, "secret.md")); err != nil {
		t.Fatal(err)
	}
	ix, err = Load(link)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ix.Docs {
		if strings.Contains(d.Body, "SECRET") {
			t.Fatalf("followed inner symlink %s", d.Rel)
		}
	}
}

func TestSeparatePosts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "posts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	early := filepath.Join(dir, "early.md")
	later := filepath.Join(dir, "later.md")
	if err := os.WriteFile(early, []byte("---\ntitle: Early\ndate: 2025-03-01T00:00:00Z\n---\nOLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(later, []byte("---\ntitle: Later\ndate: 2026-03-01T00:00:00Z\n---\nNEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(root); err != nil {
		t.Fatal(err)
	}
	early = strings.TrimSuffix(early, ".md") + ".html"
	later = strings.TrimSuffix(later, ".md") + ".html"
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	posts := ix.Posts()
	if len(posts) != 2 || posts[0].Slug != "later" || posts[1].Slug != "early" {
		t.Fatalf("posts %+v", posts)
	}
	if !posts[1].Date.Equal(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("early date %s", posts[1].Date)
	}
	if _, err := DeletePost(root, "early"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(later); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(early); !os.IsNotExist(err) {
		t.Fatalf("early still there %v", err)
	}
}

func TestGeneratedSlug(t *testing.T) {
	doc, err := WritePost(t.TempDir(), Input{Body: "note"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.Slug, "post-") {
		t.Fatalf("slug %s", doc.Slug)
	}
}

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "../outside"); !errors.Is(err, ErrEscape) {
		t.Fatalf("escape: %v", err)
	}
	if _, err := safeJoin(root, "posts/../../etc/passwd"); !errors.Is(err, ErrEscape) {
		t.Fatalf("nested escape: %v", err)
	}
	got, err := safeJoin(root, "posts/ok.md")
	if err != nil || !strings.HasPrefix(got, root) {
		t.Fatalf("join %s %v", got, err)
	}
}
