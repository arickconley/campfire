// Package server is the public HTML site and the write API.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"

	"campfire/internal/content"
	"campfire/internal/render"
	"campfire/internal/site"
	"campfire/web"
)

const maxBody = 1 << 20

// Server serves one content directory.
type Server struct {
	Root   string
	Render *render.Renderer
	// mu serializes create, update, and delete.
	mu sync.RWMutex
}

// New builds a server. root is the content directory.
// The write token is read from root/.token on each API call.
func New(root string) (*Server, error) {
	legacy, err := content.HasMarkdown(root)
	if err != nil {
		return nil, err
	}
	if legacy {
		return nil, errors.New("Markdown content found; run campfire -content <folder> -migrate before starting")
	}
	r, err := render.New()
	if err != nil {
		return nil, err
	}
	return &Server{Root: root, Render: r}, nil
}

// Handler is the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /feed.xml", s.feed)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("POST /api/posts", s.create)
	mux.HandleFunc("GET /api/posts", s.list)
	mux.HandleFunc("GET /api/posts/{slug}", s.get)
	mux.HandleFunc("PUT /api/posts/{slug}", s.update)
	mux.HandleFunc("DELETE /api/posts/{slug}", s.delete)
	mux.HandleFunc("GET /api/site/files", s.siteFiles)
	mux.HandleFunc("GET /api/site/file", s.siteFile)
	mux.HandleFunc("PUT /api/site/file", s.siteFile)
	mux.HandleFunc("GET /{path...}", s.page)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/posts" || strings.HasPrefix(r.URL.Path, "/api/posts/") || strings.HasPrefix(r.URL.Path, "/api/site/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Campfire-Content-Format", "html")
		}
		if r.URL.Path == "/favicon.ico" {
			writeFavicon(w, r)
			return
		}
		if !site.Exists(s.Root) {
			s.setup(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(web.Favicon)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(web.Favicon)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/setup" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.URL.Path == "/setup" && r.Method == http.MethodPost {
		s.mu.Lock()
		defer s.mu.Unlock()
		if site.Exists(s.Root) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		cfg := site.Config{
			Title:       r.FormValue("title"),
			Description: r.FormValue("description"),
			URL:         r.FormValue("url"),
			Style:       r.FormValue("style"),
		}
		if err := site.Write(s.Root, cfg); err != nil {
			msg := "could not write site.yaml"
			if errors.Is(err, site.ErrTitle) || errors.Is(err, site.ErrStyle) {
				msg = err.Error()
			} else {
				log.Printf("setup: %v", err)
			}
			s.renderSetup(w, r, cfg, msg, "", false, r.FormValue("api") == "1")
			return
		}
		if err := content.Rebuild(s.Root); err != nil {
			s.htmlErr(w, err)
			return
		}
		if r.FormValue("api") != "1" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		token, err := ensureToken(s.Root)
		if err != nil {
			dropSite(s.Root)
			log.Printf("setup: %v", err)
			s.renderSetup(w, r, cfg, "could not write token", "", false, true)
			return
		}
		s.renderSetup(w, r, cfg, "", token, true, true)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := site.Config{
		Title: site.DefaultTitle(s.Root),
		URL:   origin(r, site.Config{}),
		Style: "paper",
	}
	s.renderSetup(w, r, cfg, "", "", false, false)
}

// ensureToken returns the content .token, creating it on first setup.
func ensureToken(root string) (string, error) {
	token, err := site.LoadToken(root)
	if err != nil || token != "" {
		return token, err
	}
	token, err = site.GenerateToken()
	if err != nil {
		return "", err
	}
	if err := site.SaveToken(root, token); err != nil {
		return "", err
	}
	return token, nil
}

func dropSite(root string) {
	if err := os.Remove(filepath.Join(root, "site.yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("setup: %v", err)
	}
}

func (s *Server) renderSetup(w http.ResponseWriter, r *http.Request, cfg site.Config, msg, token string, done, api bool) {
	styles := site.Styles(s.Root)
	if cfg.Style == "" && len(styles) > 0 {
		cfg.Style = styles[0]
	}
	var code template.HTML
	if token != "" {
		svg, err := connectQR(reachBase(r), token)
		if err != nil {
			log.Printf("setup: %v", err)
		} else {
			code = svg
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := setupTmpl.Execute(w, setupView{Config: cfg, Styles: styles, Error: msg, Token: token, Done: done, API: api, QR: code}); err != nil {
		log.Printf("setup: %v", err)
	}
}

// reachBase is the origin a phone should call. A loopback host is replaced
// with a LAN address so the setup code works off this machine.
func reachBase(r *http.Request) string {
	scheme := requestScheme(r)
	host := r.Host
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, ""
	}
	if loopbackName(name) {
		if ip := lanIPv4(); ip != "" {
			name = ip
		}
	}
	if port != "" {
		host = net.JoinHostPort(name, port)
	} else {
		host = name
	}
	return scheme + "://" + host
}

func loopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func lanIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			return ip4.String()
		}
	}
	return ""
}

func connectQR(base, token string) (template.HTML, error) {
	payload, err := json.Marshal(struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}{URL: base, Token: token})
	if err != nil {
		return "", err
	}
	code, err := qr.Encode(string(payload), qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := code.Size + quiet*2
	var b strings.Builder
	fmt.Fprintf(&b, `<svg id="connect" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="220" height="220" role="img" aria-label="Setup code"><rect width="100%%" height="100%%" fill="#fff"/>`, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" fill="#000"/>`, x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String()), nil
}

type setupView struct {
	site.Config
	Styles []string
	Error  string
	Token  string
	Done   bool
	API    bool
	QR     template.HTML
}

var setupTmpl = template.Must(template.New("setup").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Done}}Write token{{else}}Set up{{end}}</title>
<link rel="icon" href="/favicon.ico">
<style>
  body { font-family: system-ui, sans-serif; max-width: 32rem; margin: 4rem auto; padding: 0 1rem; line-height: 1.45; }
  label { display: block; margin: 1rem 0 .25rem; }
  input[type="text"], select { width: 100%; box-sizing: border-box; font: inherit; padding: .4rem; }
  button { font: inherit; margin-top: 1rem; padding: .4rem .8rem; }
  svg { display: block; margin: 1rem 0; background: #fff; }
  #api { margin: 1.25rem .4rem 0 0; }
  label.check { display: inline; margin: 0; }
  .hint { margin: .35rem 0 0; color: #5c6b52; }
</style>
</head>
<body>
{{if .Done}}
<h1>Write token</h1>
<p>Copy this. Campfire will not show it again. It is saved as <code>.token</code> in the content folder.</p>
<input id="token" type="text" readonly value="{{.Token}}">
<p>Scan this with the Campfire app.</p>
{{.QR}}
<p><a href="/">Open the site</a></p>
{{else}}
<h1>Set up</h1>
{{if .Error}}<p><strong>{{.Error}}</strong></p>{{end}}
<form method="post" action="/setup">
<label for="title">Title</label>
<input id="title" name="title" type="text" required value="{{.Title}}">
<label for="description">Description</label>
<input id="description" name="description" type="text" value="{{.Description}}">
<label for="url">URL</label>
<input id="url" name="url" type="text" value="{{.URL}}">
<label for="style">Style</label>
<select id="style" name="style">
{{range .Styles}}<option value="{{.}}"{{if eq . $.Style}} selected{{end}}>{{.}}</option>
{{end}}</select>
<input id="api" name="api" type="checkbox" value="1"{{if .API}} checked{{end}}>
<label class="check" for="api">API access</label>
<p class="hint">Creates a write token for the app, shown after the site is created.</p>
<button type="submit">Create site.yaml</button>
</form>
{{end}}
</body>
</html>
`))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ix, cfg, ok := s.snap(w)
	if !ok {
		return
	}
	posts, err := s.articles(ix.Feed())
	if err != nil {
		s.htmlErr(w, err)
		return
	}
	v := s.view(cfg, ix)
	v.SEO = &render.SEO{URL: cfg.PublicURL("/"), Title: cfg.Title, Description: cfg.Description, SiteName: cfg.Title, Type: "website"}
	v.Posts = posts
	v.Empty = len(posts) == 0
	if len(posts) > 0 {
		v.Latest = posts[0]
		v.HasLatest = true
		v.More = posts[1:]
	}
	if b, found := ix.Blurb(); found {
		v.Blurb = template.HTML(b.Body)
		v.SEO.Body = b.Body
	}
	s.writeHTML(w, http.StatusOK, func(w io.Writer) error {
		return s.Render.Feed(s.Root, w, v)
	})
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	// Keep publication metadata and the served file consistent across API saves.
	s.mu.RLock()
	ix, cfg, ok := s.snap(w)
	if !ok {
		s.mu.RUnlock()
		return
	}
	u := cleanPath(r.PathValue("path"))
	if doc, found := ix.Page(u); found {
		if doc.Draft {
			s.mu.RUnlock()
			s.notFound(w, cfg, ix)
			return
		}
		if r.URL.Path != doc.URL {
			s.mu.RUnlock()
			target := r.URL.ResolveReference(&url.URL{Path: doc.URL, RawQuery: r.URL.RawQuery})
			http.Redirect(w, r, target.RequestURI(), http.StatusMovedPermanently)
			return
		}
		defer s.mu.RUnlock()
		w.Header().Set("Cache-Control", "no-cache")
		s.file(w, r, doc.Rel)
		return
	}
	s.mu.RUnlock()
	if rel, found := ix.Static(u); found {
		s.file(w, r, rel)
		return
	}
	s.notFound(w, cfg, ix)
}

func (s *Server) feed(w http.ResponseWriter, r *http.Request) {
	ix, cfg, ok := s.snap(w)
	if !ok {
		return
	}
	base := origin(r, cfg)
	posts := ix.Feed()
	entries := make([]render.AtomEntry, 0, len(posts))
	var updated time.Time
	for _, d := range posts {

		if updated.IsZero() {
			updated = d.Date
		}
		entries = append(entries, render.AtomEntry{
			Title:   d.Title,
			URL:     base + d.URL,
			Updated: d.Date,
			HTML:    template.HTML(d.Body),
		})
	}
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := s.Render.Atom(w, render.AtomView{
		Title:   cfg.Title,
		BaseURL: base,
		Updated: updated,
		Entries: entries,
	}); err != nil {
		log.Printf("feed: %v", err)
	}
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	ix, cfg, ok := s.snapAPI(w)
	if !ok {
		return
	}
	posts := ix.Posts()
	out := make([]postJSON, 0, len(posts))
	for _, d := range posts {
		out = append(out, toJSON(cfg, d, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	slug := r.PathValue("slug")
	ix, cfg, ok := s.snapAPI(w)
	if !ok {
		return
	}
	d, found := ix.Find(slug)
	if !found {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, toJSON(cfg, d, true))
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	in, ok := readPost(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	doc, err := content.WritePost(s.Root, in)
	s.mu.Unlock()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	cfg, err := site.Load(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "site")
		return
	}
	writeJSON(w, http.StatusCreated, toJSON(cfg, doc, true))
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	slug := r.PathValue("slug")
	in, ok := readPost(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	doc, err := content.UpdatePost(s.Root, slug, in)
	s.mu.Unlock()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	cfg, err := site.Load(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "site")
		return
	}
	writeJSON(w, http.StatusOK, toJSON(cfg, doc, true))
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	slug := r.PathValue("slug")
	s.mu.Lock()
	_, err := content.DeletePost(s.Root, slug)
	s.mu.Unlock()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) bool {
	token, err := site.LoadToken(s.Root)
	if err != nil {
		log.Printf("token: %v", err)
		writeErr(w, http.StatusInternalServerError, "token")
		return false
	}
	if token == "" {
		writeErr(w, http.StatusForbidden, "writes disabled")
		return false
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || subtle.ConstantTimeCompare([]byte(h[:len(prefix)]), []byte(prefix)) != 1 {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	got := h[len(prefix):]
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (s *Server) snap(w http.ResponseWriter) (*content.Index, site.Config, bool) {
	ix, cfg, err := s.load()
	if err != nil {
		s.htmlErr(w, err)
		return nil, site.Config{}, false
	}
	return ix, cfg, true
}

func (s *Server) snapAPI(w http.ResponseWriter) (*content.Index, site.Config, bool) {
	ix, cfg, err := s.load()
	if err != nil {
		log.Printf("load: %v", err)
		writeErr(w, http.StatusInternalServerError, "content")
		return nil, site.Config{}, false
	}
	return ix, cfg, true
}

func (s *Server) load() (*content.Index, site.Config, error) {
	ix, err := content.Load(s.Root)
	if err != nil {
		return nil, site.Config{}, err
	}
	cfg, err := site.Load(s.Root)
	if err != nil {
		return nil, site.Config{}, err
	}
	return ix, cfg, nil
}

func (s *Server) articles(docs []content.Doc) ([]render.Article, error) {
	out := make([]render.Article, 0, len(docs))
	for _, d := range docs {
		out = append(out, article(d, template.HTML(d.Body)))
	}
	return out, nil
}

func (s *Server) view(cfg site.Config, ix *content.Index) render.View {
	return render.View{
		SiteTitle:  cfg.Title,
		SiteDesc:   cfg.Description,
		HeadTitle:  cfg.Title,
		Style:      styleHref(cfg, ix),
		PagesLinks: pageLinks(ix),
	}
}

func pageLinks(ix *content.Index) []render.PageLink {
	docs := ix.Pages()
	out := make([]render.PageLink, 0, len(docs))
	for _, d := range docs {
		title := d.Title
		if title == "" {
			title = strings.TrimPrefix(d.URL, "/")
		}
		out = append(out, render.PageLink{Title: title, URL: d.URL})
	}
	return out
}

// styleHref picks the stylesheet from the content folder.
// "paper" → /styles/paper.css. "style.css" or "styles/mine.css" is that path.
// An empty style uses /style.css when that file exists.
func styleHref(cfg site.Config, ix *content.Index) string {
	name := strings.TrimSpace(cfg.Style)
	rel := "style.css"
	if name != "" {
		if strings.Contains(name, "/") || strings.HasSuffix(strings.ToLower(name), ".css") {
			rel = strings.TrimPrefix(path.Clean("/"+name), "/")
		} else {
			rel = "styles/" + name + ".css"
		}
	}
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") {
		return ""
	}
	urlPath := "/" + rel
	if _, ok := ix.Static(urlPath); !ok {
		return ""
	}
	return urlPath
}

func (s *Server) writeHTML(w http.ResponseWriter, code int, fn func(io.Writer) error) {
	var buf strings.Builder
	if err := fn(&buf); err != nil {
		s.htmlErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(code)
	io.WriteString(w, buf.String())
}

func (s *Server) notFound(w http.ResponseWriter, cfg site.Config, ix *content.Index) {
	v := s.view(cfg, ix)
	v.Back = "/"
	s.writeHTML(w, http.StatusNotFound, func(w io.Writer) error {
		return s.Render.NotFound(s.Root, w, v)
	})
}

// backPath is where BackButton points. Posts return home. A nested page
// returns its parent when that page is published.
func backPath(ix *content.Index, urlPath string) string {
	if urlPath == "" || urlPath == "/" {
		return ""
	}
	if strings.HasPrefix(urlPath, "/posts/") && strings.Count(strings.Trim(urlPath, "/"), "/") == 1 {
		return "/"
	}
	parent := path.Dir(urlPath)
	if parent == "." || parent == "/" {
		return "/"
	}
	if d, ok := ix.Page(parent); ok && !d.Draft {
		return parent
	}
	return "/"
}

func (s *Server) htmlErr(w http.ResponseWriter, err error) {
	log.Printf("render: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, rel string) {
	root, err := filepath.Abs(s.Root)
	if err != nil {
		s.htmlErr(w, err)
		return
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

func article(d content.Doc, html template.HTML) render.Article {
	a := render.Article{
		Title:     d.Title,
		URL:       d.URL,
		HTML:      html,
		ShowTitle: d.Title != "",
		ShowDate:  d.Dated,
	}
	if d.Dated {
		a.Date = d.Date.UTC().Format("2 January 2006")
		a.DateISO = d.Date.UTC().Format("2006-01-02")
	}
	return a
}

func readPost(w http.ResponseWriter, r *http.Request) (content.Input, bool) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") && ct != "application/vnd.campfire.html+json" && ct != "application/vnd.campfire.description+json" {
		writeErr(w, http.StatusUnsupportedMediaType, "content type")
		return content.Input{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var req struct {
		MetaDescription *string `json:"metaDescription"`
		Title           string  `json:"title"`
		Slug            string  `json:"slug"`
		Date            string  `json:"date"`
		Draft           bool    `json:"draft"`
		Body            string  `json:"body"`
		Format          string  `json:"format"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "body too large")
			return content.Input{}, false
		}
		writeErr(w, http.StatusBadRequest, "invalid json")
		return content.Input{}, false
	}
	if req.Format != "html" {
		writeErr(w, http.StatusBadRequest, "HTML body format required; update your Campfire app")
		return content.Input{}, false
	}
	in := content.Input{MetaDescription: req.MetaDescription, Title: req.Title, Slug: req.Slug, Draft: req.Draft, Body: req.Body}
	if req.Date != "" {
		t, err := time.Parse(time.RFC3339, req.Date)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid date")
			return content.Input{}, false
		}
		in.Date = t
	}
	return in, true
}

type postJSON struct {
	MetaDescription string `json:"metaDescription"`
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	Date            string `json:"date"`
	Draft           bool   `json:"draft"`
	URL             string `json:"url"`
	Body            string `json:"body,omitempty"`
	Format          string `json:"format"`
}

func toJSON(cfg site.Config, d content.Doc, withBody bool) postJSON {
	p := postJSON{
		MetaDescription: d.MetaDescription,
		Format:          "html",
		Slug:            d.Slug,
		Title:           d.Title,
		Date:            d.Date.UTC().Format(time.RFC3339),
		Draft:           d.Draft,
		URL:             postURL(cfg, d.URL),
	}
	if withBody {
		p.Body = d.Body
	}
	return p
}

func postURL(cfg site.Config, urlPath string) string {
	if cfg.URL == "" {
		return urlPath
	}
	return cfg.URL + urlPath
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, content.ErrSiteFile):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, content.ErrExists):
		writeErr(w, http.StatusConflict, "post exists")
	case errors.Is(err, content.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, content.ErrSlug):
		writeErr(w, http.StatusBadRequest, "invalid slug")
	case errors.Is(err, content.ErrHTML):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, content.ErrBody):
		writeErr(w, http.StatusBadRequest, "body required")
	case errors.Is(err, content.ErrEscape):
		writeErr(w, http.StatusBadRequest, "invalid path")
	default:
		log.Printf("store: %v", err)
		writeErr(w, http.StatusInternalServerError, "store")
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func cleanPath(p string) string {
	p = path.Clean("/" + p)
	if p == "." {
		return "/"
	}
	return p
}

func requestScheme(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := r.Header.Get("X-Forwarded-Proto"); xf == "http" || xf == "https" {
		scheme = xf
	}
	return scheme
}

func origin(r *http.Request, cfg site.Config) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	scheme := requestScheme(r)
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}
