// Package render builds complete HTML pages and Atom feeds.
package render

import (
	"bytes"
	"encoding/xml"
	"errors"
	"html"
	"html/template"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

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

// Renderer holds the site templates. Post bodies are already HTML.
type Renderer struct {
	tmpl           *template.Template
	overrideName   string
	overrideSource string
}

// WithTemplate validates syntax and previews an override without changing files.
func (r *Renderer) WithTemplate(name, source string) (*Renderer, error) {
	if _, err := template.New(name).Parse(source); err != nil {
		return nil, err
	}
	return &Renderer{tmpl: r.tmpl, overrideName: name, overrideSource: source}, nil
}

func New() (*Renderer, error) {
	tmpl, err := template.ParseFS(web.FS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{tmpl: tmpl}, nil
}

// Feed writes the home page.
func (r *Renderer) Feed(root string, w io.Writer, v View) error {
	return r.write(root, w, "blog_posts", v)
}

// Page builds one complete HTML page.
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
		if r.overrideName != "" {
			_, err = set.New(r.overrideName).Parse(r.overrideSource)
		}
		return set, err
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
	if r.overrideName != "" {
		if _, err := set.New(r.overrideName).Parse(r.overrideSource); err != nil {
			return nil, err
		}
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
