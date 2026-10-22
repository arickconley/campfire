package server

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"campfire/internal/content"
	"campfire/internal/site"
)

func TestSEOEndpointsAndMetadata(t *testing.T) {
	s, root := htmlServer(t)
	if err := site.Write(root, site.Config{Title: "Test & Site", Description: "A site summary.", URL: "https://blog.example"}); err != nil {
		t.Fatal(err)
	}
	// Existing custom templates need no new placeholders and stale metadata is replaced.
	layout := `<html><head><title>{{.HeadTitle}}</title><link rel="canonical" href="https://wrong.example/"><meta property="og:title" content="Wrong"><meta name="description" content="Wrong"></head><body>{{.Main}}</body></html>`
	if err := os.WriteFile(filepath.Join(root, "templates/layout.html"), []byte(layout), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"format":"html","slug":"visible","title":"A & B","metaDescription":"A quoted \"summary\".","body":"<p>Keep this.</p><p><img src=\"/media/photo.jpg\" alt=\"A photo\"/></p>"}`,
		`{"format":"html","slug":"secret","draft":true,"body":"<p>Private</p>"}`,
	} {
		if got := htmlRequest(s, "POST", "/api/posts", input); got.Code != 201 {
			t.Fatalf("create: %d %s", got.Code, got.Body)
		}
	}
	if err := content.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/", "/posts/visible", "/about"} {
		got := htmlRequest(s, "GET", route, "")
		if got.Code != 200 {
			t.Fatalf("%s: %d", route, got.Code)
		}
		page := got.Body.String()
		canonical := "https://blog.example" + route
		if strings.Count(page, `rel="canonical"`) != 1 || !strings.Contains(page, `href="`+canonical+`"`) || !strings.Contains(page, `property="og:url" content="`+canonical+`"`) || strings.Contains(page, "wrong.example") {
			t.Fatalf("canonical %s: %s", route, page)
		}
		if strings.Count(page, `property="og:title"`) != 1 {
			t.Fatal("duplicate OG title")
		}
		if route == "/posts/visible" {
			for _, want := range []string{`property="og:title" content="A &amp; B"`, `property="og:type" content="article"`, `property="og:description" content="A quoted &#34;summary&#34;."`, `property="og:image" content="https://blog.example/media/photo.jpg"`, `<p>Keep this.</p>`} {
				if !strings.Contains(page, want) {
					t.Fatalf("missing %s: %s", want, page)
				}
			}
		}
		if route == "/" && !strings.Contains(page, `name="description" content="A site summary."`) {
			t.Fatal("missing home description")
		}
	}
	response := htmlRequest(s, "GET", "/sitemap.xml", "")
	var sitemap struct {
		XMLName   xml.Name
		Locations []string `xml:"url>loc"`
	}
	if response.Code != 200 {
		t.Fatalf("sitemap: %d", response.Code)
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &sitemap); err != nil {
		t.Fatal(err)
	}
	if sitemap.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
		t.Fatalf("namespace: %+v", sitemap)
	}
	joined := strings.Join(sitemap.Locations, "\n")
	for _, want := range []string{"https://blog.example/", "https://blog.example/about", "https://blog.example/posts/visible"} {
		found := false
		for _, loc := range sitemap.Locations {
			if loc == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "secret") || strings.Contains(joined, ".html") || strings.Contains(joined, "/styles/") {
		t.Fatalf("private/noncanonical URLs: %s", joined)
	}
	for _, method := range []string{"GET", "HEAD"} {
		got := htmlRequest(s, method, "/posts/visible/?from=test", "")
		if got.Code != 301 || got.Header().Get("Location") != "/posts/visible?from=test" {
			t.Fatalf("redirect: %d %s", got.Code, got.Header())
		}
	}
	if got := htmlRequest(s, "GET", "/posts/secret/", ""); got.Code != 404 {
		t.Fatal("draft URL leaked")
	}
	if got := htmlRequest(s, "GET", "/missing", ""); got.Code != 404 || strings.Contains(got.Body.String(), `href="https://blog.example/"`) {
		t.Fatal("404 got home canonical")
	}
	if got := htmlRequest(s, "DELETE", "/api/posts/visible", ""); got.Code != 204 {
		t.Fatal("delete failed")
	}
	if got := htmlRequest(s, "GET", "/sitemap.xml", ""); strings.Contains(got.Body.String(), "/posts/visible") {
		t.Fatal("sitemap did not update")
	}
}

func TestRobotsAndMissingPublicURL(t *testing.T) {
	s, root := htmlServer(t)
	// Never generate public URLs from an arbitrary request Host.
	req := httptest.NewRequest("GET", "https://attacker.example/sitemap.xml", nil)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code != 503 || strings.Contains(response.Body.String(), "attacker.example") {
		t.Fatal("unconfigured sitemap used request Host")
	}
	if got := htmlRequest(s, "GET", "/robots.txt", ""); got.Code != 200 || strings.Contains(got.Body.String(), "Sitemap:") {
		t.Fatal("unconfigured robots")
	}
	if err := site.Write(root, site.Config{Title: "SEO test", URL: "https://blog.example"}); err != nil {
		t.Fatal(err)
	}
	want := "Sitemap: https://blog.example/sitemap.xml"
	if got := htmlRequest(s, "GET", "/robots.txt", ""); !strings.Contains(got.Body.String(), want) {
		t.Fatal("missing sitemap declaration")
	}
	for _, custom := range []string{"User-agent: *\nDisallow: /private/\n", "User-agent: *\nDisallow: /\n\n" + want + "\n"} {
		if err := os.WriteFile(filepath.Join(root, "robots.txt"), []byte(custom), 0600); err != nil {
			t.Fatal(err)
		}
		got := htmlRequest(s, "GET", "/robots.txt", "")
		if !strings.HasPrefix(got.Body.String(), custom) || strings.Count(got.Body.String(), want) != 1 {
			t.Fatalf("custom rules changed: %s", got.Body)
		}
	}
	for _, route := range []string{"/sitemap.xml", "/robots.txt"} {
		got := htmlRequest(s, http.MethodHead, route, "")
		if got.Code != 200 || got.Body.Len() != 0 {
			t.Fatalf("HEAD %s: %d %s", route, got.Code, got.Body)
		}
	}
}
