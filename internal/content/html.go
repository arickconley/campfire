package content

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"campfire/internal/render"
	"campfire/internal/site"
)

var ErrHTML = errors.New("unsupported or unsafe HTML")

// NormalizeHTML accepts the editor's HTML subset and rejects active content.
// A real HTML parser handles entities, malformed nesting, and attribute quoting.
func NormalizeHTML(body string) (string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(body), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", ErrHTML
	}
	var check func(*html.Node, int) error
	check = func(n *html.Node, depth int) error {
		if depth > 64 {
			return ErrHTML
		}
		invalidXML := func(r rune) bool {
			return r < 0x20 && r != '\n' && r != '\r' && r != '\t' || r == 0xfffe || r == 0xffff
		}
		if strings.IndexFunc(n.Data, invalidXML) >= 0 {
			return ErrHTML
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "pre", "code", "strong", "em", "b", "i", "s", "del", "br", "hr", "a", "img", "table", "thead", "tbody", "tr", "th", "td", "input":
			default:
				return fmt.Errorf("%w: <%s>", ErrHTML, n.Data)
			}
			if n.Data == "input" {
				disabled := false
				for _, a := range n.Attr {
					if a.Key == "disabled" {
						disabled = true
					}
				}
				if attribute(n, "type") != "checkbox" || !disabled {
					return ErrHTML
				}
			}
			for _, a := range n.Attr {
				if a.Namespace != "" || strings.IndexFunc(a.Val, invalidXML) >= 0 {
					return ErrHTML
				}
				switch {
				case n.Data == "input" && (a.Key == "type" || a.Key == "checked" || a.Key == "disabled"):
				case n.Data == "a" && a.Key == "href", n.Data == "img" && a.Key == "src":
					value := strings.TrimSpace(a.Val)
					if strings.IndexFunc(value, unicode.IsControl) >= 0 {
						return ErrHTML
					}
					u, err := url.Parse(value)
					if err != nil {
						return ErrHTML
					}
					scheme := strings.ToLower(u.Scheme)
					if scheme != "" && scheme != "http" && scheme != "https" && !(n.Data == "a" && scheme == "mailto") {
						return ErrHTML
					}
				case (n.Data == "a" || n.Data == "img") && a.Key == "title", n.Data == "img" && a.Key == "alt":
				case n.Data == "ol" && a.Key == "start":
					if _, err := strconv.Atoi(a.Val); err != nil {
						return ErrHTML
					}
				case n.Data == "code" && a.Key == "class":
					if !strings.HasPrefix(a.Val, "language-") || strings.ContainsAny(a.Val, " \t\r\n") {
						return ErrHTML
					}
				case (n.Data == "td" || n.Data == "th") && a.Key == "align":
					if a.Val != "left" && a.Val != "right" && a.Val != "center" {
						return ErrHTML
					}
				default:
					return fmt.Errorf("%w: attribute %s", ErrHTML, a.Key)
				}
			}
		} else if n.Type != html.TextNode && n.Type != html.CommentNode {
			return ErrHTML
		}
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.CommentNode {
				n.RemoveChild(c)
				c = next
				continue
			}
			if err := check(c, depth+1); err != nil {
				return err
			}
			c = next
		}
		return nil
	}
	var out bytes.Buffer
	for _, n := range nodes {
		if err := check(n, 0); err != nil {
			return "", err
		}
		if n.Type != html.CommentNode {
			if err := html.Render(&out, n); err != nil {
				return "", err
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func parseHTML(rel string, mod time.Time, raw []byte) (Doc, error) {
	tree, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return Doc{}, err
	}
	meta := map[string]string{}
	var body *html.Node
	invalid := false
	walk(tree, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "head" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode || c.Data != "meta" {
					continue
				}
				name := attribute(c, "name")
				if !strings.HasPrefix(name, "campfire-") {
					continue
				}
				if _, exists := meta[name]; exists {
					invalid = true
				}
				meta[name] = attribute(c, "content")
			}
		}
		for _, a := range n.Attr {
			if a.Key == "data-campfire-body" {
				if body != nil {
					invalid = true
				}
				body = n
			}
		}
	})
	title, titled := meta["campfire-title"]
	date, dated := meta["campfire-date"]
	draft, drafted := meta["campfire-draft"]
	if invalid || body == nil || !titled || !dated || !drafted || (draft != "true" && draft != "false") {
		return Doc{}, errors.New("invalid Campfire HTML metadata or missing body")
	}
	var b bytes.Buffer
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return Doc{}, err
		}
	}
	source, err := NormalizeHTML(b.String())
	if err != nil {
		return Doc{}, err
	}
	kind, u, slug := classify(rel)
	doc := Doc{Rel: rel, URL: u, Slug: slug, Title: title, Draft: draft == "true", Body: source, Kind: kind}
	if date != "" {
		doc.Date, err = time.Parse(time.RFC3339, date)
		if err != nil {
			return Doc{}, err
		}
		doc.Dated = true
	} else if kind == KindPost {
		doc.Date = mod.UTC()
		doc.Dated = true
	}
	return doc, nil
}

// encode builds the complete document once, before the atomic file replacement.
func encode(root string, doc Doc) ([]byte, error) {
	r, err := render.New()
	if err != nil {
		return nil, err
	}
	return encodeWithRenderer(root, doc, r)
}

func encodeWithRenderer(root string, doc Doc, r *render.Renderer) ([]byte, error) {
	body, err := NormalizeHTML(doc.Body)
	if err != nil {
		return nil, err
	}
	cfg, err := site.Load(root)
	if err != nil {
		return nil, err
	}
	ix, err := Load(root)
	if err != nil {
		return nil, err
	}
	v := render.View{SiteTitle: cfg.Title, SiteDesc: cfg.Description, HeadTitle: cfg.Title, Back: "/"}
	if cfg.Style != "" {
		style := cfg.Style
		if !strings.Contains(style, "/") && !strings.HasSuffix(style, ".css") {
			style = "styles/" + style + ".css"
		}
		if _, ok := ix.Static("/" + strings.TrimPrefix(style, "/")); ok {
			v.Style = "/" + strings.TrimPrefix(style, "/")
		}
	} else if _, ok := ix.Static("/style.css"); ok {
		v.Style = "/style.css"
	}
	for _, p := range ix.Pages() {
		title := p.Title
		if title == "" {
			title = strings.TrimPrefix(p.URL, "/")
		}
		v.PagesLinks = append(v.PagesLinks, render.PageLink{Title: title, URL: p.URL})
	}
	if doc.Kind != KindPost {
		parent := path.Dir(doc.URL)
		if parent != "/" {
			if p, ok := ix.Page(parent); ok && !p.Draft {
				v.Back = parent
			}
		}
	}
	if doc.Title != "" {
		v.HeadTitle = doc.Title + " · " + cfg.Title
	}
	a := render.Article{Title: doc.Title, URL: doc.URL, ShowTitle: doc.Title != "", ShowDate: doc.Dated,
		HTML: template.HTML(`<div data-campfire-body="">` + body + `</div>`)}
	if doc.Dated {
		a.Date = doc.Date.UTC().Format("2 January 2006")
		a.DateISO = doc.Date.UTC().Format("2006-01-02")
	}
	v.Article = a
	var page bytes.Buffer
	if err := r.Page(root, &page, v); err != nil {
		return nil, err
	}
	tree, err := html.Parse(&page)
	if err != nil {
		return nil, err
	}
	var head *html.Node
	walk(tree, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "head" {
			head = n
		}
	})
	if head == nil {
		return nil, errors.New("page template has no head")
	}
	date := ""
	if doc.Dated {
		date = doc.Date.UTC().Format(time.RFC3339)
	}
	for _, pair := range [][2]string{{"campfire-title", doc.Title}, {"campfire-date", date}, {"campfire-draft", fmt.Sprint(doc.Draft)}} {
		head.AppendChild(&html.Node{Type: html.ElementNode, Data: "meta", DataAtom: atom.Meta, Attr: []html.Attribute{{Key: "name", Val: pair[0]}, {Key: "content", Val: pair[1]}}})
	}
	var out bytes.Buffer
	if err := html.Render(&out, tree); err != nil {
		return nil, err
	}
	// A custom template must include .Article.HTML or saving would lose the editable source.
	parsed, err := parseHTML(doc.Rel, doc.Date, out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("page template must preserve .Article.HTML: %w", err)
	}
	if parsed.Body != body {
		return nil, errors.New("page template changed the editable body")
	}
	return out.Bytes(), nil
}
