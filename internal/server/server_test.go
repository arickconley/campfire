package server

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"campfire/internal/content"
	"campfire/internal/site"
)

func boot(t *testing.T, token string, files map[string]string) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if token != "" {
		if err := os.WriteFile(filepath.Join(root, ".token"), []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := content.Migrate(root); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, root
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func call(t *testing.T, method, url, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func sample() map[string]string {
	return map[string]string{
		"site.yaml":        "title: Dadaches\ndescription: Notes.\nurl: http://example.test\nstyle: paper\n",
		"styles/paper.css": "/* paper */\nbody{color:#17262c}\n",
		"index.md":         "---\ntitle: Home\n---\nBlurb text.\n",
		"about.md":         "---\ntitle: About\n---\nAbout text.\n",
		"posts/new.md":     "---\ntitle: New\ndate: 2026-06-01T00:00:00Z\n---\nNEWPOST\n",
		"posts/old.md":     "---\ntitle: Old\ndate: 2026-01-01T00:00:00Z\n---\nOLDPOST\n",
		"posts/secret.md":  "---\ntitle: Secret\ndraft: true\ndate: 2026-10-01T00:00:00Z\n---\nDRAFTSECRET\n",
		"media/mark.svg":   "<svg id=\"mark\"></svg>\n",
		"_hidden.md":       "HIDDENSECRET\n",
	}
}

func TestSeededSite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "content")
	if err := content.Seed(root); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	code, body := get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(body, `action="/setup"`) || !strings.Contains(body, `<option value="nerd">`) || !strings.Contains(body, `<option value="paper" selected>`) || !strings.Contains(body, `href="/favicon.ico"`) {
		t.Fatalf("setup %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/favicon.ico")
	if code != 200 || !strings.HasPrefix(body, "\x00\x00\x01\x00") {
		t.Fatalf("favicon %d", code)
	}
	res, err := http.PostForm(ts.URL+"/setup", url.Values{
		"title": {"Camp"},
		"style": {"paper"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	page := string(b)
	if res.StatusCode != 200 || !strings.Contains(page, "The fire is lit.") || !strings.Contains(page, "Short notes, newest first.") || !strings.Contains(page, `href="/styles/paper.css"`) || !strings.Contains(page, `href="/favicon.ico"`) || strings.Contains(page, `action="/setup"`) || strings.Contains(page, "mark.svg") {
		t.Fatalf("home %d %s", res.StatusCode, page)
	}
	code, _ = get(t, ts.URL+"/templates/layout.html")
	if code != 404 {
		t.Fatalf("template served %d", code)
	}
	code, body = get(t, ts.URL+"/about")
	if code != 200 || !strings.Contains(body, "microblog served from this folder") {
		t.Fatalf("about %d %s", code, body)
	}
	if saved, err := site.LoadToken(root); err != nil || saved != "" {
		t.Fatalf("token %q %v", saved, err)
	}
}

func TestPageLinks(t *testing.T) {
	files := sample()
	files["later.md"] = "---\ntitle: Later\n---\nLATER\n"
	files["notes.md"] = "just notes\n"
	files["gone.md"] = "---\ntitle: Gone\ndraft: true\n---\nGONE\n"
	ts, _ := boot(t, "", files)

	code, body := get(t, ts.URL+"/")
	nav := navHTML(t, body)
	about := strings.Index(nav, `href="/about">About</a>`)
	later := strings.Index(nav, `href="/later">Later</a>`)
	notes := strings.Index(nav, `href="/notes">notes</a>`)
	feed := strings.Index(nav, `href="/feed.xml"`)
	if code != 200 || about < 0 || later < about || notes < later || feed < notes || strings.Contains(nav, "Gone") || strings.Contains(nav, "/posts/") {
		t.Fatalf("nav %d %s", code, nav)
	}
	code, body = get(t, ts.URL+"/missing")
	if code != 404 || !strings.Contains(navHTML(t, body), `href="/about">About</a>`) {
		t.Fatalf("404 nav %d %s", code, body)
	}
}

func navHTML(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "<nav>")
	j := strings.Index(body, "</nav>")
	if i < 0 || j < i {
		t.Fatalf("nav %s", body)
	}
	return body[i : j+len("</nav>")]
}

func TestPublicPages(t *testing.T) {
	ts, _ := boot(t, "secret", sample())

	code, body := get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(body, "NEWPOST") || !strings.Contains(body, "Blurb text.") {
		t.Fatalf("home %d %s", code, body)
	}
	if !strings.Contains(body, `href="/styles/paper.css"`) || strings.Contains(body, "class=") {
		t.Fatalf("style link %s", body)
	}
	if !strings.Contains(body, `datetime="2026-06-01"`) {
		t.Fatalf("time %s", body)
	}
	if strings.Contains(body, "DRAFTSECRET") || strings.Contains(body, "HIDDENSECRET") {
		t.Fatalf("leaked %s", body)
	}
	newAt := strings.Index(body, "NEWPOST")
	oldAt := strings.Index(body, "OLDPOST")
	if newAt < 0 || oldAt < 0 || newAt > oldAt {
		t.Fatalf("order new=%d old=%d", newAt, oldAt)
	}

	code, body = get(t, ts.URL+"/posts/new")
	if code != 200 || !strings.Contains(body, "NEWPOST") || !strings.Contains(body, "New") {
		t.Fatalf("post %d %s", code, body)
	}
	if !strings.Contains(body, `<a href="/">Back</a>`) {
		t.Fatalf("back %s", body)
	}
	code, body = get(t, ts.URL+"/about")
	if code != 200 || !strings.Contains(body, "About text.") {
		t.Fatalf("about %d %s", code, body)
	}
	code, _ = get(t, ts.URL+"/posts/secret")
	if code != 404 {
		t.Fatalf("draft %d", code)
	}
	code, _ = get(t, ts.URL+"/_hidden")
	if code != 404 {
		t.Fatalf("hidden %d", code)
	}
	code, _ = get(t, ts.URL+"/site.yaml")
	if code != 404 {
		t.Fatalf("site.yaml %d", code)
	}
	code, body = get(t, ts.URL+"/media/mark.svg")
	if code != 200 || !strings.Contains(body, "mark") {
		t.Fatalf("svg %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/styles/paper.css")
	if code != 200 || !strings.Contains(body, "/* paper */") {
		t.Fatalf("css %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/feed.xml")
	if code != 200 || !strings.Contains(body, "http://example.test/posts/new") || strings.Contains(body, "secret") {
		t.Fatalf("feed %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/missing")
	if code != 404 || !strings.Contains(body, "Not found") {
		t.Fatalf("404 %d %s", code, body)
	}
}

func TestAPI(t *testing.T) {
	ts, root := boot(t, "secret", sample())

	code, body := call(t, http.MethodPost, ts.URL+"/api/posts", "", `{"format":"html","body":"x"}`)
	if code != 401 {
		t.Fatalf("no token %d %s", code, body)
	}
	code, body = call(t, http.MethodPost, ts.URL+"/api/posts", "nope", `{"format":"html","body":"x"}`)
	if code != 401 {
		t.Fatalf("bad token %d %s", code, body)
	}
	code, body = call(t, http.MethodPost, ts.URL+"/api/posts", "secret", `{"format":"html","slug":"../x","body":"no"}`)
	if code != 400 {
		t.Fatalf("traversal %d %s", code, body)
	}
	code, body = call(t, http.MethodPost, ts.URL+"/api/posts", "secret", `{"format":"html","title":"Hello","body":"First post."}`)
	if code != 201 || !strings.Contains(body, `"slug":"hello"`) {
		t.Fatalf("create %d %s", code, body)
	}
	var created struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "http://example.test/posts/hello" {
		t.Fatalf("url %s", created.URL)
	}
	code, body = call(t, http.MethodPost, ts.URL+"/api/posts", "secret", `{"format":"html","slug":"hello","body":"again"}`)
	if code != 409 {
		t.Fatalf("dup %d %s", code, body)
	}

	code, home := get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(home, "First post.") {
		t.Fatalf("after create %d %s", code, home)
	}

	code, body = call(t, http.MethodGet, ts.URL+"/api/posts", "secret", "")
	if code != 200 || !strings.Contains(body, "secret") || !strings.Contains(body, "hello") {
		t.Fatalf("list %d %s", code, body)
	}
	code, body = call(t, http.MethodGet, ts.URL+"/api/posts/secret", "secret", "")
	if code != 200 || !strings.Contains(body, "DRAFTSECRET") {
		t.Fatalf("get draft %d %s", code, body)
	}

	code, body = call(t, http.MethodPut, ts.URL+"/api/posts/hello", "secret", `{"format":"html","title":"Edited","body":"Updated body"}`)
	if code != 200 || !strings.Contains(body, "Updated body") {
		t.Fatalf("put %d %s", code, body)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "posts", "*hello.html"))
	if len(matches) != 1 {
		t.Fatalf("files %v", matches)
	}
	code, home = get(t, ts.URL+"/posts/hello")
	if code != 200 || !strings.Contains(home, "Updated body") {
		t.Fatalf("edited page %d %s", code, home)
	}

	code, _ = call(t, http.MethodDelete, ts.URL+"/api/posts/hello", "secret", "")
	if code != 204 {
		t.Fatalf("delete %d", code)
	}
	code, _ = get(t, ts.URL+"/posts/hello")
	if code != 404 {
		t.Fatalf("gone %d", code)
	}
	code, _ = call(t, http.MethodDelete, ts.URL+"/api/posts/hello", "secret", "")
	if code != 404 {
		t.Fatalf("delete missing %d", code)
	}
}

func TestStyleChoice(t *testing.T) {
	files := sample()
	files["site.yaml"] = "title: Dadaches\nstyle: style.css\n"
	files["style.css"] = "/* custom */\n"
	ts, _ := boot(t, "", files)
	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `href="/style.css"`) || strings.Contains(body, "/styles/paper.css") {
		t.Fatalf("custom %s", body)
	}

	files = sample()
	delete(files, "styles/paper.css")
	files["site.yaml"] = "title: Dadaches\n"
	files["style.css"] = "/* root */\n"
	ts, _ = boot(t, "", files)
	_, body = get(t, ts.URL+"/")
	if !strings.Contains(body, `href="/style.css"`) {
		t.Fatalf("default file %s", body)
	}

	files = sample()
	files["site.yaml"] = "title: Dadaches\nstyle: missing\n"
	ts, _ = boot(t, "", files)
	_, body = get(t, ts.URL+"/")
	if strings.Contains(body, "stylesheet") {
		t.Fatalf("missing style still linked %s", body)
	}
}

func TestBackPath(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"about.md":          "---\ntitle: About\n---\nAbout\n",
		"now/index.md":      "---\ntitle: Now\n---\nNow\n",
		"now/week.md":       "week\n",
		"posts/hello.md":    "hello\n",
		"deep/gone/leaf.md": "leaf\n",
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
	if err := content.Migrate(root); err != nil {
		t.Fatal(err)
	}
	ix, err := content.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if backPath(ix, "/") != "" || backPath(ix, "/posts/hello") != "/" || backPath(ix, "/about") != "/" {
		t.Fatalf("home/post/about")
	}
	if backPath(ix, "/now/week") != "/now" {
		t.Fatalf("parent %s", backPath(ix, "/now/week"))
	}
	if backPath(ix, "/deep/gone/leaf") != "/" {
		t.Fatalf("missing parent %s", backPath(ix, "/deep/gone/leaf"))
	}
}

func TestTemplates(t *testing.T) {
	files := sample()
	files["templates/blog_posts.html"] = "{{if .HasLatest}}<p id=\"latest\">{{.Latest.HTML}}</p>{{end}}<ul>{{range .More}}<li>{{.Title}}</li>{{end}}</ul>"
	files["templates/blog_post.html"] = "<article id=\"one\">{{.Article.Title}}{{.Article.HTML}}</article>"
	ts, _ := boot(t, "", files)
	code, body := get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(body, `id="latest"`) || !strings.Contains(body, "NEWPOST") {
		t.Fatalf("latest %d %s", code, body)
	}
	if strings.Contains(body, "OLDPOST") || !strings.Contains(body, "<li>Old</li>") {
		t.Fatalf("more %s", body)
	}
	code, _ = get(t, ts.URL+"/templates/blog_posts.html")
	if code != 404 {
		t.Fatalf("template served %d", code)
	}
	code, body = get(t, ts.URL+"/posts/new")
	if code != 200 || !strings.Contains(body, `id="one"`) || !strings.Contains(body, "New") {
		t.Fatalf("post template %d %s", code, body)
	}

	files["templates/blog_posts.html"] = "{{.Missing"
	ts, _ = boot(t, "", files)
	code, body = get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(body, "NEWPOST") || !strings.Contains(body, "OLDPOST") {
		t.Fatalf("bad template %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/posts/new")
	if code != 200 || !strings.Contains(body, `id="one"`) {
		t.Fatalf("good template dropped %d %s", code, body)
	}
}

func TestReachBase(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://blog.example:8080/", nil)
	r.Host = "blog.example:8080"
	if got := reachBase(r); got != "http://blog.example:8080" {
		t.Fatalf("host %s", got)
	}
	r.Host = "127.0.0.1:8080"
	got := reachBase(r)
	if ip := lanIPv4(); ip != "" {
		if !strings.Contains(got, ip) || strings.Contains(got, "127.0.0.1") {
			t.Fatalf("lan %s", got)
		}
	} else if got != "http://127.0.0.1:8080" {
		t.Fatalf("loopback %s", got)
	}
	r.TLS = &tls.ConnectionState{}
	r.Host = "blog.example:8443"
	if got := reachBase(r); got != "https://blog.example:8443" {
		t.Fatalf("tls %s", got)
	}
	r.TLS = nil
	r.Host = "thoughts.arick.io"
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := reachBase(r); got != "https://thoughts.arick.io" {
		t.Fatalf("forwarded %s", got)
	}
	r.Header.Set("X-Forwarded-Proto", "nope")
	if got := reachBase(r); got != "http://thoughts.arick.io" {
		t.Fatalf("bad forwarded %s", got)
	}
}

func TestSetupPage(t *testing.T) {
	files := sample()
	delete(files, "site.yaml")
	files["styles/ink.css"] = "/* ink */\n"
	files["styles/plain.css"] = "/* plain */\n"
	ts, root := boot(t, "", files)

	code, body := get(t, ts.URL+"/")
	titleAt := strings.Index(body, `id="title"`)
	apiAt := strings.Index(body, `id="api"`)
	if code != 200 || !strings.Contains(body, `action="/setup"`) || strings.Contains(body, "NEWPOST") || titleAt < 0 || apiAt < titleAt {
		t.Fatalf("form %d title %d api %d %s", code, titleAt, apiAt, body)
	}
	if strings.Contains(body, `id="token"`) || strings.Contains(body, `id="connect"`) || strings.Contains(body, `value="1" checked`) {
		t.Fatalf("token shown before opt-in %s", body)
	}
	saved, err := site.LoadToken(root)
	if err != nil || saved != "" {
		t.Fatalf("token written on get %q %v", saved, err)
	}
	code, body = get(t, ts.URL+"/")
	saved, err = site.LoadToken(root)
	if code != 200 || err != nil || saved != "" || strings.Contains(body, `id="connect"`) {
		t.Fatalf("token written on refresh %q %v", saved, err)
	}
	if !strings.Contains(body, `<option value="ink">`) || !strings.Contains(body, `<option value="paper" selected>`) || strings.Contains(body, "Writes site.yaml") {
		t.Fatalf("styles %s", body)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(ts.URL + "/posts/new")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("redirect %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	res, err = client.PostForm(ts.URL+"/setup", url.Values{"title": {""}, "style": {"ink"}, "api": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(b), "title is required") || !strings.Contains(string(b), `value="1" checked`) {
		t.Fatalf("empty title %d %s", res.StatusCode, b)
	}
	if site.Exists(root) {
		t.Fatal("wrote yaml on error")
	}
	if saved, err := site.LoadToken(root); err != nil || saved != "" {
		t.Fatalf("token written on error %q %v", saved, err)
	}

	res, err = client.PostForm(ts.URL+"/setup", url.Values{
		"title":       {"Dadaches"},
		"description": {"Notes."},
		"url":         {"http://example.test/"},
		"style":       {"ink"},
		"api":         {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	page := string(b)
	if res.StatusCode != 200 || !strings.Contains(page, `id="token"`) || !strings.Contains(page, `id="connect"`) {
		t.Fatalf("created %d %s", res.StatusCode, page)
	}
	token := tokenFrom(t, page)
	got, err := site.LoadToken(root)
	if err != nil || got != token {
		t.Fatalf("saved %q %v", got, err)
	}
	cfg, err := site.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Dadaches" || cfg.Description != "Notes." || cfg.URL != "http://example.test" || cfg.Style != "ink" {
		t.Fatalf("yaml %+v", cfg)
	}
	code, body = call(t, http.MethodPost, ts.URL+"/api/posts", token, `{"format":"html","title":"Hi","body":"From the wizard."}`)
	if code != 201 {
		t.Fatalf("write with wizard token %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/")
	if code != 200 || !strings.Contains(body, "NEWPOST") || !strings.Contains(body, `href="/styles/ink.css"`) || strings.Contains(body, token) {
		t.Fatalf("blog %d %s", code, body)
	}
	code, _ = get(t, ts.URL+"/.token")
	if code != 404 {
		t.Fatalf(".token served %d", code)
	}
}

func TestSetupSkipsToken(t *testing.T) {
	files := sample()
	delete(files, "site.yaml")
	ts, root := boot(t, "", files)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.PostForm(ts.URL+"/setup", url.Values{
		"title": {"Dadaches"},
		"style": {"paper"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("redirect %d %s %s", res.StatusCode, res.Header.Get("Location"), b)
	}
	if strings.Contains(string(b), `id="token"`) || strings.Contains(string(b), `id="connect"`) {
		t.Fatalf("showed token %s", b)
	}
	if saved, err := site.LoadToken(root); err != nil || saved != "" {
		t.Fatalf("token written %q %v", saved, err)
	}
	code, body := call(t, http.MethodGet, ts.URL+"/api/posts", "secret", "")
	if code != 403 {
		t.Fatalf("api %d %s", code, body)
	}
	code, body = get(t, ts.URL+"/")
	if code != 200 || strings.Contains(body, `id="connect"`) || strings.Contains(body, `action="/setup"`) {
		t.Fatalf("home %d %s", code, body)
	}
}

func tokenFrom(t *testing.T, page string) string {
	t.Helper()
	const mark = `id="token" type="text" readonly value="`
	i := strings.Index(page, mark)
	if i < 0 {
		t.Fatalf("no token field %s", page)
	}
	rest := page[i+len(mark):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("token field %s", page)
	}
	return rest[:j]
}

func TestConcurrentCreate(t *testing.T) {
	ts, root := boot(t, "secret", sample())
	var wg sync.WaitGroup
	codes := make([]int, 2)
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/posts", strings.NewReader(`{"format":"html","slug":"race","body":"x"}`))
			if err != nil {
				errs[i] = err
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer secret")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				errs[i] = err
				return
			}
			defer res.Body.Close()
			io.Copy(io.Discard, res.Body)
			codes[i] = res.StatusCode
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Ints(codes)
	if codes[0] != http.StatusCreated || codes[1] != http.StatusConflict {
		t.Fatalf("codes %v", codes)
	}
	matches, err := filepath.Glob(filepath.Join(root, "posts", "*race.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("files %v", matches)
	}
}

func TestWritesDisabled(t *testing.T) {
	ts, _ := boot(t, "", sample())
	code, body := call(t, http.MethodGet, ts.URL+"/api/posts", "secret", "")
	if code != 403 || !strings.Contains(body, "writes disabled") {
		t.Fatalf("%d %s", code, body)
	}
}
