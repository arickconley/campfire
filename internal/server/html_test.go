package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"campfire/internal/content"
	"campfire/internal/site"
)

// These exercise the real HTTP handler without requiring a listening socket.
func htmlServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	if err := content.Seed(root); err != nil {
		t.Fatal(err)
	}
	if err := site.Write(root, site.Config{Title: "HTML Site", Style: "paper"}); err != nil {
		t.Fatal(err)
	}
	if err := site.SaveToken(root, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := content.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func htmlRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	if body != "" {
		request.Header.Set("Content-Type", "application/vnd.campfire.html+json")
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	return response
}

func TestHTMLPostLifecycle(t *testing.T) {
	s, root := htmlServer(t)
	input := `{"format":"html","slug":"a-page","title":"A & B","date":"2026-10-18T14:00:00Z","body":"<p>Hello <strong>world</strong>.</p>"}`
	created := htmlRequest(s, http.MethodPost, "/api/posts", input)
	if created.Header().Get("X-Campfire-Content-Format") != "html" {
		t.Fatal("missing API format header")
	}
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var post postJSON
	if err := json.Unmarshal(created.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	if post.Format != "html" || post.Body != "<p>Hello <strong>world</strong>.</p>" {
		t.Fatalf("post: %+v", post)
	}
	full := filepath.Join(root, "posts/a-page.html")
	stored, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"<!DOCTYPE html>", `name="campfire-title" content="A &amp; B"`, `name="campfire-date" content="2026-10-18T14:00:00Z"`, `name="campfire-draft" content="false"`, `data-campfire-body=""`} {
		if !strings.Contains(string(stored), part) {
			t.Fatalf("missing %s in %s", part, stored)
		}
	}
	public := htmlRequest(s, http.MethodGet, "/posts/a-page", "")
	if public.Code != 200 || public.Body.String() != string(stored) {
		t.Fatalf("not served directly: %d %s", public.Code, public.Body)
	}
	read := htmlRequest(s, http.MethodGet, "/api/posts/a-page", "")
	if err := json.Unmarshal(read.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	if post.Body != "<p>Hello <strong>world</strong>.</p>" || post.Title != "A & B" || post.Date != "2026-10-18T14:00:00Z" {
		t.Fatalf("round trip: %+v", post)
	}
	if _, err := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(err) {
		t.Fatalf("unexpected HTML cache: %v", err)
	}

	// A template change takes effect only after rebuilding the saved document.
	tmpl := `<article id="new-layout">{{.Article.HTML}}</article>`
	if err := os.WriteFile(filepath.Join(root, "templates/blog_post.html"), []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := htmlRequest(s, http.MethodGet, "/posts/a-page", ""); got.Body.String() != string(stored) {
		t.Fatal("request rebuilt the page")
	}
	if err := content.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	if got := htmlRequest(s, http.MethodGet, "/posts/a-page", ""); !strings.Contains(got.Body.String(), `id="new-layout"`) {
		t.Fatal("rebuild did not apply template")
	}

	updated := htmlRequest(s, http.MethodPut, "/api/posts/a-page", `{"format":"html","title":"Updated","date":"2026-10-18T14:00:00Z","body":"<p>Updated body.</p>"}`)
	if updated.Code != 200 {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	for _, path := range []string{"/posts/a-page", "/", "/feed.xml"} {
		got := htmlRequest(s, http.MethodGet, path, "")
		if got.Code != 200 || !strings.Contains(got.Body.String(), "Updated body.") {
			t.Fatalf("stale %s: %d %s", path, got.Code, got.Body)
		}
	}
	if got := htmlRequest(s, http.MethodDelete, "/api/posts/a-page", ""); got.Code != 204 {
		t.Fatalf("delete: %d", got.Code)
	}
	if got := htmlRequest(s, http.MethodGet, "/posts/a-page", ""); got.Code != 404 {
		t.Fatalf("deleted post: %d", got.Code)
	}
}

func TestHTMLDraftAndWriteBoundaries(t *testing.T) {
	s, root := htmlServer(t)
	created := htmlRequest(s, http.MethodPost, "/api/posts", `{"format":"html","slug":"private","title":"Secret","draft":true,"body":"<p>PRIVATE-CONTENT</p>"}`)
	if created.Code != 201 {
		t.Fatalf("create: %s", created.Body)
	}
	for _, path := range []string{"/posts/private", "/posts/private.html", "/.markdown-backup/posts/private.md"} {
		got := htmlRequest(s, http.MethodGet, path, "")
		if got.Code != 404 || strings.Contains(got.Body.String(), "PRIVATE-CONTENT") {
			t.Fatalf("draft leaked at %s", path)
		}
	}
	for _, path := range []string{"/", "/feed.xml"} {
		if got := htmlRequest(s, http.MethodGet, path, ""); strings.Contains(got.Body.String(), "PRIVATE-CONTENT") {
			t.Fatalf("draft leaked at %s", path)
		}
	}
	original, err := os.ReadFile(filepath.Join(root, "posts/private.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`<script>alert(1)</script>`, `<img src="/x" onerror="alert(1)"/>`, `<a href="jav&#x61;script:alert(1)">bad</a>`, `<svg onload="alert(1)"></svg>`, `<meta name="campfire-draft" content="false"/>`} {
		input, _ := json.Marshal(map[string]any{"format": "html", "body": body})
		got := htmlRequest(s, http.MethodPut, "/api/posts/private", string(input))
		if got.Code != 400 {
			t.Fatalf("unsafe HTML accepted: %s: %d %s", body, got.Code, got.Body)
		}
	}
	oldClient := htmlRequest(s, http.MethodPut, "/api/posts/private", `{"body":"**old Markdown client**"}`)
	if oldClient.Code != 400 {
		t.Fatalf("old client: %d", oldClient.Code)
	}
	after, _ := os.ReadFile(filepath.Join(root, "posts/private.html"))
	if string(after) != string(original) {
		t.Fatal("failed update changed saved content")
	}
	// A template that drops the editable region cannot destroy a saved body.
	if err := os.WriteFile(filepath.Join(root, "templates/blog_post.html"), []byte(`<article>{{.Article.Title}}</article>`), 0o644); err != nil {
		t.Fatal(err)
	}
	failed := htmlRequest(s, http.MethodPut, "/api/posts/private", `{"format":"html","body":"<p>Replacement</p>"}`)
	if failed.Code != 500 {
		t.Fatalf("source-dropping template accepted: %d", failed.Code)
	}
	after, _ = os.ReadFile(filepath.Join(root, "posts/private.html"))
	if string(after) != string(original) {
		t.Fatal("template failure changed saved content")
	}
}

func TestHTMLLegacyMigrationIsExplicit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "posts"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "---\ntitle: Old post\ndate: 2025-01-02T03:04:05Z\ndraft: true\n---\nKeep **bold** and [links](/about).\n"
	if err := os.WriteFile(filepath.Join(root, "posts/old.md"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil {
		t.Fatal("legacy content silently accepted")
	}
	if err := content.Migrate(root); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(root, ".markdown-backup/posts/old.md"))
	if err != nil || string(backup) != source {
		t.Fatal("original was not preserved")
	}
	ix, err := content.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := ix.Find("old")
	if !ok || !doc.Draft || doc.Title != "Old post" || !strings.Contains(doc.Body, "<strong>bold</strong>") || doc.Date.UTC().Format("2006-01-02T15:04:05Z") != "2025-01-02T03:04:05Z" {
		t.Fatalf("migration: %+v", doc)
	}
	if err := content.Migrate(root); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}

type pausedHTMLWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	resume  chan struct{}
}

func (w *pausedHTMLWriter) WriteHeader(status int) {
	close(w.entered)
	<-w.resume
	w.ResponseRecorder.WriteHeader(status)
}

func TestHTMLPublicationSnapshot(t *testing.T) {
	s, _ := htmlServer(t)
	if got := htmlRequest(s, http.MethodPost, "/api/posts", `{"format":"html","slug":"snapshot","body":"<p>Public version.</p>"}`); got.Code != 201 {
		t.Fatal(got.Body)
	}
	writer := &pausedHTMLWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/posts/snapshot", nil))
	}()
	<-writer.entered
	// A write cannot unpublish the page and replace its body during this response.
	if s.mu.TryLock() {
		s.mu.Unlock()
		close(writer.resume)
		<-done
		t.Fatal("serving a page did not protect its publication snapshot")
	}
	close(writer.resume)
	<-done
	if !strings.Contains(writer.Body.String(), "Public version.") {
		t.Fatal(writer.Body)
	}
	if got := htmlRequest(s, http.MethodPut, "/api/posts/snapshot", `{"format":"html","draft":true,"body":"<p>Private edits.</p>"}`); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if got := htmlRequest(s, http.MethodGet, "/posts/snapshot", ""); got.Code != 404 || strings.Contains(got.Body.String(), "Private edits.") {
		t.Fatal("unpublished content leaked")
	}
}
