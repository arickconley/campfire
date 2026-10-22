package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"campfire/internal/content"
)

func TestSiteEditor(t *testing.T) {
	s, root := htmlServer(t)
	read := func(path string) content.SiteFile {
		t.Helper()
		got := htmlRequest(s, "GET", "/api/site/file?path="+url.QueryEscape(path), "")
		if got.Code != 200 {
			t.Fatalf("read %s: %d %s", path, got.Code, got.Body)
		}
		var file content.SiteFile
		if err := json.Unmarshal(got.Body.Bytes(), &file); err != nil {
			t.Fatal(err)
		}
		return file
	}
	save := func(file content.SiteFile) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(file)
		if err != nil {
			t.Fatal(err)
		}
		return htmlRequest(s, "PUT", "/api/site/file?path="+url.QueryEscape(file.Path), string(body))
	}
	for _, endpoint := range []string{"/api/site/files", "/api/site/file?path=about.html"} {
		for _, method := range []string{"GET", "PUT"} {
			if method == "PUT" && endpoint == "/api/site/files" {
				continue
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(method, endpoint, nil))
			if w.Code != 401 {
				t.Fatalf("unauthenticated %s: %d", endpoint, w.Code)
			}
		}
	}
	listed := htmlRequest(s, "GET", "/api/site/files", "")
	if listed.Code != 200 || listed.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(listed)
	}
	for _, want := range []string{"index.html", "about.html", "templates/layout.html", "styles/paper.css"} {
		if !strings.Contains(listed.Body.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(listed.Body.String(), "posts/hello") {
		t.Fatal("posts included in page editor")
	}
	for _, path := range []string{".token", "site.yaml", "../.token", "posts/hello.html", "styles/../.token"} {
		got := htmlRequest(s, "GET", "/api/site/file?path="+url.QueryEscape(path), "")
		if got.Code != 404 {
			t.Fatalf("private path %s: %d", path, got.Code)
		}
	}
	for _, path := range []string{"about.html", "index.html"} {
		file := read(path)
		file.Body = "<p>Edited <strong>page</strong>.</p>"
		file.Title = "Edited"
		if got := save(file); got.Code != 200 {
			t.Fatal(got.Body)
		}
		if got := save(file); got.Code != 409 {
			t.Fatalf("stale save: %d", got.Code)
		}
		publicPath := "/about"
		if path == "index.html" {
			publicPath = "/"
		}
		if got := htmlRequest(s, "GET", publicPath, ""); !strings.Contains(got.Body.String(), file.Body) {
			t.Fatal(got.Body)
		}
		invalid := read(path)
		invalid.Body = "<script>alert(1)</script>"
		if got := save(invalid); got.Code != 400 {
			t.Fatalf("unsafe page: %d", got.Code)
		}
		if read(path).Body != file.Body {
			t.Fatal("invalid save lost content")
		}
	}
	about := read("about.html")
	about.Draft = true
	if got := save(about); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if got := htmlRequest(s, "GET", "/about", ""); got.Code != 404 {
		t.Fatal("draft page was published")
	}
	index := read("index.html")
	index.Body = ""
	if got := save(index); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if read("index.html").Body != "" {
		t.Fatal("home blurb could not be cleared")
	}
	tooBig := read("styles/paper.css")
	tooBig.Body = strings.Repeat("x", maxBody)
	if got := save(tooBig); got.Code != 413 {
		t.Fatalf("oversized file: %d", got.Code)
	}
	css := read("styles/paper.css")
	css.Body = "body { color: red; }"
	if got := save(css); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if got := htmlRequest(s, "GET", "/styles/paper.css", ""); got.Body.String() != css.Body {
		t.Fatal("CSS unchanged")
	}
	layout := read("templates/blog_post.html")
	original, err := os.ReadFile(filepath.Join(root, "posts/hello.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"{{end}}", "<article>{{.Article.Title}}</article>", "{{.Missing}}"} {
		layout.Body = invalid
		if got := save(layout); got.Code != 400 {
			t.Fatalf("invalid template: %d %s", got.Code, got.Body)
		}
		current, _ := os.ReadFile(filepath.Join(root, "posts/hello.html"))
		if string(original) != string(current) {
			t.Fatal("bad template changed post")
		}
	}
	layout.Body = `<article id="edited-template">{{.Article.HTML}}</article>`
	if got := save(layout); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if got := htmlRequest(s, "GET", "/posts/hello", ""); !strings.Contains(got.Body.String(), "edited-template") || !strings.Contains(got.Body.String(), "The fire is lit.") {
		t.Fatal(got.Body)
	}
	for _, path := range []string{"templates/blog_list.html", "templates/blog_posts.html", "templates/notfound.html"} {
		bad := read(path)
		bad.Body = "{{.Missing}}"
		if got := save(bad); got.Code != 400 {
			t.Fatalf("bad %s: %d", path, got.Code)
		}
	}
	// A missing override is editable using the starter source.
	if err := os.Remove(filepath.Join(root, "templates/notfound.html")); err != nil {
		t.Fatal(err)
	}
	missing := read("templates/notfound.html")
	if missing.Revision != "missing" {
		t.Fatal(missing)
	}
	missing.Body = `<h1>Gone</h1>`
	if got := save(missing); got.Code != 200 {
		t.Fatal(got.Body)
	}
	// Links must not expose secrets even when they use an allowed filename.
	if err := os.Symlink(filepath.Join(root, ".token"), filepath.Join(root, "styles/secret.css")); err != nil {
		t.Fatal(err)
	}
	if got := htmlRequest(s, "GET", "/api/site/file?path=styles/secret.css", ""); got.Code != 404 {
		t.Fatal("symlink exposed")
	}
	if err := os.RemoveAll(filepath.Join(root, "templates")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "templates")); err != nil {
		t.Fatal(err)
	}
	if got := htmlRequest(s, "GET", "/api/site/file?path=templates/layout.html", ""); got.Code != 404 {
		t.Fatal("template symlink exposed")
	}
}
