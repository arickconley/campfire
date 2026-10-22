package content

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"campfire/internal/render"
	"campfire/internal/site"
)

var ErrConflict = errors.New("this file changed on the server; reopen it before saving")
var ErrSiteFile = errors.New("invalid site file")

// SiteFile exposes page bodies, stylesheet source, and known template source only.
type SiteFile struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Draft    bool   `json:"draft"`
	Body     string `json:"body"`
	Revision string `json:"revision"`
}

var editorTemplates = []string{"layout", "blog_posts", "blog_list", "blog_post", "notfound"}

func SiteFiles(root string) ([]SiteFile, error) {
	ix, err := Load(root)
	if err != nil {
		return nil, err
	}
	out := []SiteFile{}
	for _, d := range ix.Docs {
		if d.Kind != KindPost {
			out = append(out, SiteFile{Path: d.Rel, Kind: "page", Title: d.Title, Draft: d.Draft})
		}
	}
	for _, rel := range ix.static {
		if strings.HasSuffix(rel, ".css") {
			out = append(out, SiteFile{Path: rel, Kind: "style"})
		}
	}
	for _, name := range editorTemplates {
		rel := "templates/" + name + ".html"
		if _, err := editorPath(root, rel); err == nil {
			out = append(out, SiteFile{Path: rel, Kind: "template"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// editorPath rejects hidden paths and symlinks, including links to other files inside root.
func editorPath(root, rel string) (string, error) {
	if !fs.ValidPath(rel) || strings.Contains(rel, "\\") {
		return "", ErrSiteFile
	}
	full := root
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return "", ErrSiteFile
		}
		full = filepath.Join(full, part)
		info, err := os.Lstat(full)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ErrSiteFile
		}
	}
	return full, nil
}

func ReadSiteFile(root, rel string) (SiteFile, error) {
	files, err := SiteFiles(root)
	if err != nil {
		return SiteFile{}, err
	}
	for _, f := range files {
		if f.Path != rel {
			continue
		}
		full, err := editorPath(root, rel)
		if err != nil {
			return SiteFile{}, err
		}
		raw, err := os.ReadFile(full)
		if os.IsNotExist(err) && f.Kind == "template" {
			raw, err = defaultFS.ReadFile("defaults/" + rel)
			f.Revision = "missing"
		} else {
			f.Revision = fmt.Sprintf("%x", sha256.Sum256(raw))
		}
		if err != nil {
			return SiteFile{}, err
		}
		f.Body = string(raw)
		if f.Kind == "page" {
			info, err := os.Stat(full)
			if err != nil {
				return SiteFile{}, err
			}
			doc, err := parseHTML(rel, info.ModTime(), raw)
			if err != nil {
				return SiteFile{}, err
			}
			f.Body, f.Title, f.Draft = doc.Body, doc.Title, doc.Draft
		}
		return f, nil
	}
	return SiteFile{}, ErrNotFound
}

func UpdateSiteFile(root, rel string, in SiteFile) (SiteFile, error) {
	current, err := ReadSiteFile(root, rel)
	if err != nil {
		return SiteFile{}, err
	}
	if in.Revision == "" || in.Revision != current.Revision {
		return SiteFile{}, ErrConflict
	}
	if !utf8.ValidString(in.Body) || strings.ContainsRune(in.Body, 0) {
		return SiteFile{}, ErrSiteFile
	}
	full, err := editorPath(root, rel)
	if err != nil {
		return SiteFile{}, err
	}
	switch current.Kind {
	case "page":
		ix, err := Load(root)
		if err != nil {
			return SiteFile{}, err
		}
		for _, doc := range ix.Docs {
			if doc.Rel != rel {
				continue
			}
			doc.Body, doc.Title, doc.Draft = in.Body, in.Title, in.Draft
			if err := save(root, &doc); err != nil {
				return SiteFile{}, err
			}
		}
	case "template":
		if strings.TrimSpace(in.Body) == "" {
			return SiteFile{}, ErrSiteFile
		}
		if err := updateTemplate(root, rel, in.Body); err != nil {
			return SiteFile{}, err
		}
	case "style":
		if err := writeAtomic(filepath.Dir(full), full, []byte(in.Body)); err != nil {
			return SiteFile{}, err
		}
	}
	return ReadSiteFile(root, rel)
}

func updateTemplate(root, rel, source string) error {
	r, err := render.New()
	if err != nil {
		return err
	}
	r, err = r.WithTemplate(strings.TrimSuffix(path.Base(rel), ".html"), source)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSiteFile, err)
	}
	ix, err := Load(root)
	if err != nil {
		return err
	}
	// Render every document before writing anything; a bad template cannot lose source.
	pages := make(map[string][]byte, len(ix.Docs))
	for _, doc := range ix.Docs {
		data, err := encodeWithRenderer(root, doc, r)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrSiteFile, doc.Rel, err)
		}
		pages[doc.Rel] = data
	}
	cfg, err := site.Load(root)
	if err != nil {
		return err
	}
	sample := render.Article{Title: "Page", HTML: template.HTML("<p>Content</p>"), ShowTitle: true, ShowDate: true}
	v := render.View{SiteTitle: cfg.Title, SiteDesc: cfg.Description, Article: sample, Posts: []render.Article{sample}, Latest: sample, HasLatest: true, More: []render.Article{sample}, Back: "/"}
	if _, err := encodeWithRenderer(root, Doc{Rel: "about.html", Kind: KindPage, Body: "<p>Content</p>"}, r); err != nil {
		return fmt.Errorf("%w: %v", ErrSiteFile, err)
	}
	for _, empty := range []bool{false, true} {
		v.Empty = empty
		v.HasLatest = !empty
		if empty {
			v.Posts = nil
			v.More = nil
		}
		if err := r.Feed(root, io.Discard, v); err != nil {
			return fmt.Errorf("%w: %v", ErrSiteFile, err)
		}
	}
	if err := r.NotFound(root, io.Discard, v); err != nil {
		return fmt.Errorf("%w: %v", ErrSiteFile, err)
	}
	full, err := editorPath(root, rel)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Dir(full), full, []byte(source)); err != nil {
		return err
	}
	for rel, data := range pages {
		full, err := editorPath(root, rel)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Dir(full), full, data); err != nil {
			return fmt.Errorf("template saved, but rebuilding %s failed; run -rebuild: %w", rel, err)
		}
	}
	return nil
}
