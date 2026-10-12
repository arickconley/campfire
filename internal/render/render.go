// Package render turns markdown and site data into HTML and Atom.
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"campfire/web"
)

// PageLink is one published page for {{range .PagesLinks}}.
type PageLink struct {
	Title string
	URL   string
}

// Article is one rendered post or page.
type Article struct {
	Title     string
	Date      string
	DateISO   string
	URL       string
	HTML      template.HTML
	ShowDate  bool
	ShowTitle bool
}

// View is the data for an HTML page.
type View struct {
	SiteTitle  string
	SiteDesc   string
	HeadTitle  string
	Style      string
	Blurb      template.HTML
	Posts      []Article
	Latest     Article
	HasLatest  bool
	More       []Article
	Article    Article
	PagesLinks []PageLink
	Empty      bool
	Main       template.HTML
	// Back is the URL for BackButton. Empty on the home page.
	Back string
}

// BackButton is the {{.BackButton}} tag. It is empty when Back is empty.
func (v View) BackButton() template.HTML {
	if v.Back == "" {
		return ""
	}
	return template.HTML(`<p><a href="` + html.EscapeString(v.Back) + `">Back</a></p>`)
}

// AtomView is a feed.
type AtomView struct {
	Title   string
	BaseURL string
	Updated time.Time
	Entries []AtomEntry
}

// AtomEntry is one feed item.
type AtomEntry struct {
	Title   string
	URL     string
	Updated time.Time
	HTML    template.HTML
}

// Renderer holds the markdown engine and templates.
type Renderer struct {
	md   goldmark.Markdown
	tmpl *template.Template
}

// New parses the embedded templates.
func New() (*Renderer, error) {
	tmpl, err := template.ParseFS(web.FS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(escapeHTML{}, 1000)),
		),
	)
	return &Renderer{md: md, tmpl: tmpl}, nil
}

// Markdown renders src. Raw HTML is escaped into text.
func (r *Renderer) Markdown(src string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// Cached renders src and stores one HTML file at root/.cache/<rel>.html.
// The file records a hash of src and the renderer version. An edit replaces that file.
// The HTML is not kept on the Renderer. A cache write error is logged.
func (r *Renderer) Cached(root, rel, src string) (template.HTML, error) {
	if root == "" {
		return r.Markdown(src)
	}
	full, err := cachePath(root, rel)
	if err != nil {
		return "", err
	}
	stamp := cacheStamp(src)
	if b, err := os.ReadFile(full); err == nil {
		if got, html, ok := splitStamp(b); ok && got == stamp {
			return template.HTML(html), nil
		}
	}
	html, err := r.Markdown(src)
	if err != nil {
		return "", err
	}
	if err := writeCache(full, stamp, []byte(html)); err != nil {
		log.Printf("cache %s: %v", rel, err)
		return html, nil
	}
	return html, nil
}

// Drop removes the cached HTML for rel. A missing file is not an error.
func Drop(root, rel string) {
	full, err := cachePath(root, rel)
	if err != nil {
		return
	}
	os.Remove(full)
}

func splitStamp(b []byte) (string, []byte, bool) {
	i := bytes.IndexByte(b, '\n')
	if i <= 0 {
		return "", nil, false
	}
	return string(b[:i]), b[i+1:], true
}

// cacheStamp is the hash of src plus the markdown renderer version.
// A Goldmark upgrade misses every old file.
func cacheStamp(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:]) + " " + renderVersion
}

var renderVersion = goldmarkVersion()

func goldmarkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path != "github.com/yuin/goldmark" {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Version != "" {
			return dep.Version
		}
	}
	return "unknown"
}

func writeCache(full, stamp string, html []byte) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_, werr := f.Write(append([]byte(stamp+"\n"), html...))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(name, full); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func cachePath(root, rel string) (string, error) {
	rel = path.Clean("/" + filepath.ToSlash(rel))
	if rel == "/" {
		return "", fmt.Errorf("cache path %q", rel)
	}
	rel = strings.TrimPrefix(rel, "/")
	cacheRoot := filepath.Join(root, ".cache")
	full := filepath.Join(cacheRoot, filepath.FromSlash(rel)+".html")
	absRoot, err := filepath.Abs(cacheRoot)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+sep) {
		return "", fmt.Errorf("cache path %q", rel)
	}
	return full, nil
}

// Feed writes the home page.
func (r *Renderer) Feed(root string, w io.Writer, v View) error {
	return r.write(root, w, "blog_posts", v)
}

// Page writes one markdown page.
func (r *Renderer) Page(root string, w io.Writer, v View) error {
	return r.write(root, w, "blog_post", v)
}

// NotFound writes the 404 page. The caller sets the status code.
func (r *Renderer) NotFound(root string, w io.Writer, v View) error {
	v.HeadTitle = "Not found · " + v.SiteTitle
	return r.write(root, w, "notfound", v)
}

// templateNames are the files a content directory may override.
// The file body is the template. No {{define}} wrapper.
var templateNames = []string{"layout", "blog_posts", "blog_list", "blog_post", "notfound"}

func (r *Renderer) write(root string, w io.Writer, name string, v View) error {
	set, err := r.set(root)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, name, v); err != nil {
		return err
	}
	v.Main = template.HTML(buf.String())
	return set.ExecuteTemplate(w, "layout", v)
}

// set clones the built-in templates and overlays content/templates.
func (r *Renderer) set(root string) (*template.Template, error) {
	set, err := r.tmpl.Clone()
	if err != nil {
		return nil, err
	}
	if root == "" {
		return set, nil
	}
	dir := filepath.Join(root, "templates")
	for _, name := range templateNames {
		b, err := os.ReadFile(filepath.Join(dir, name+".html"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		next, err := set.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := next.New(name).Parse(string(b)); err != nil {
			log.Printf("templates/%s.html: %v", name, err)
			continue
		}
		set = next
	}
	return set, nil
}

// Atom writes an Atom 1.0 document.
func (r *Renderer) Atom(w io.Writer, v AtomView) error {
	updated := v.Updated.UTC()
	if updated.IsZero() {
		updated = time.Now().UTC()
	}
	feed := atomFeed{
		NS:      "http://www.w3.org/2005/Atom",
		Title:   v.Title,
		ID:      v.BaseURL + "/",
		Updated: updated.Format(time.RFC3339),
		Links: []atomLink{
			{Rel: "self", Href: v.BaseURL + "/feed.xml"},
			{Rel: "alternate", Href: v.BaseURL + "/"},
		},
	}
	for _, e := range v.Entries {
		title := e.Title
		if title == "" {
			title = e.Updated.UTC().Format("2 January 2006")
		}
		when := e.Updated.UTC()
		if when.IsZero() {
			when = updated
		}
		feed.Entries = append(feed.Entries, atomEntry{
			Title:   title,
			ID:      e.URL,
			Updated: when.Format(time.RFC3339),
			Link:    atomLink{Rel: "alternate", Href: e.URL},
			Content: atomContent{Type: "html", Body: string(e.HTML)},
		})
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(feed)
}

// escapeHTML turns raw HTML nodes into text so the renderer escapes them.
type escapeHTML struct{}

func (escapeHTML) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var nodes []ast.Node
	var vals [][]byte
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var raw []byte
		switch x := n.(type) {
		case *ast.HTMLBlock:
			raw = x.Text(src)
		case *ast.RawHTML:
			raw = x.Text(src)
		default:
			return ast.WalkContinue, nil
		}
		nodes = append(nodes, n)
		vals = append(vals, append([]byte(nil), raw...))
		return ast.WalkSkipChildren, nil
	})
	for i, n := range nodes {
		p := n.Parent()
		p.InsertBefore(p, n, ast.NewString(vals[i]))
		p.RemoveChild(p, n)
	}
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	NS      string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Content atomContent `xml:"content"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}
