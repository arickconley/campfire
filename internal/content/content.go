// Package content reads and writes complete HTML documents.
// The content directory is the database.
package content

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const postsDir = "posts"

var (
	ErrEscape   = errors.New("path escapes content root")
	ErrExists   = errors.New("post exists")
	ErrNotFound = errors.New("post not found")
	ErrSlug     = errors.New("invalid slug")
	ErrBody     = errors.New("body required")

	slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// Kind is how a content file is published.
type Kind int

const (
	KindPage Kind = iota
	KindPost
	KindBlurb
)

// Doc is one HTML document with its editable body.
type Doc struct {
	Rel   string
	URL   string
	Slug  string
	Title string
	Date  time.Time
	Dated bool
	Draft bool
	Body  string
	Kind  Kind
}

// Input is a post write.
// A zero Date means now.
type Input struct {
	Title string
	Slug  string
	Date  time.Time
	Draft bool
	Body  string
}

// Index is one scan of a content directory.
type Index struct {
	Docs   []Doc
	byURL  map[string]Doc
	static map[string]string // URL path → relative file
}

type front struct {
	Title string    `yaml:"title"`
	Date  time.Time `yaml:"date"`
	Draft bool      `yaml:"draft"`
}

// Load walks root. Hidden names (dot or underscore) and site.yaml are skipped.
// Symlinks inside the tree are skipped. A symlinked root is followed.
// Invalid HTML documents are skipped and never exposed as static files.
func Load(root string) (*Index, error) { return load(root, false) }

func load(root string, legacy bool) (*Index, error) {
	ix := &Index{
		byURL:  map[string]Doc{},
		static: map[string]string{},
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(rootAbs, func(full string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rootAbs, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		base := d.Name()
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || (d.IsDir() && base == "templates") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if base == "site.yaml" {
			return nil
		}
		if strings.HasSuffix(base, ".md") && !legacy {
			return nil
		}
		if strings.HasSuffix(base, ".html") && legacy {
			return nil
		}
		if strings.HasSuffix(base, ".html") || (legacy && strings.HasSuffix(base, ".md")) {
			raw, err := os.ReadFile(full)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			var doc Doc
			if legacy {
				doc, err = parseMarkdown(rel, info.ModTime(), raw)
			} else {
				doc, err = parseHTML(rel, info.ModTime(), raw)
			}
			if err != nil {
				if legacy {
					return fmt.Errorf("parse %s: %w", rel, err)
				}
				log.Printf("skip %s: %v", rel, err)
				return nil
			}
			ix.Docs = append(ix.Docs, doc)
			if old, ok := ix.byURL[doc.URL]; !ok || winsURL(doc, old) {
				ix.byURL[doc.URL] = doc
			}
			return nil
		}
		ix.static["/"+rel] = rel
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ix, nil
}

// Page returns the HTML doc for a URL path such as /about or /posts/hello.
func (ix *Index) Page(urlPath string) (Doc, bool) {
	d, ok := ix.byURL[urlPath]
	if !ok || d.Kind == KindBlurb {
		return Doc{}, false
	}
	return d, true
}

// Static returns the content-relative path for an asset URL.
func (ix *Index) Static(urlPath string) (string, bool) {
	rel, ok := ix.static[urlPath]
	return rel, ok
}

// Blurb is content/index.html when it is published.
func (ix *Index) Blurb() (Doc, bool) {
	for _, d := range ix.Docs {
		if d.Kind == KindBlurb && !d.Draft {
			return d, true
		}
	}
	return Doc{}, false
}

// Posts returns every post, including drafts, newest first.
// A repeated slug keeps the file that wins the public URL.
func (ix *Index) Posts() []Doc {
	var out []Doc
	seen := map[string]bool{}
	for _, d := range ix.Docs {
		if d.Kind != KindPost || seen[d.Slug] {
			continue
		}
		if cur, ok := ix.byURL[d.URL]; ok && cur.Rel != d.Rel {
			continue
		}
		seen[d.Slug] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Pages returns published pages, by URL.
// The home blurb and posts are not included. Drafts are not included.
func (ix *Index) Pages() []Doc {
	var out []Doc
	seen := map[string]bool{}
	for _, d := range ix.Docs {
		if d.Kind != KindPage || d.Draft || seen[d.URL] {
			continue
		}
		if cur, ok := ix.byURL[d.URL]; ok && cur.Rel != d.Rel {
			continue
		}
		seen[d.URL] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].URL < out[j].URL
	})
	return out
}

// Feed returns published posts, newest first.
func (ix *Index) Feed() []Doc {
	var out []Doc
	for _, d := range ix.Posts() {
		if !d.Draft {
			out = append(out, d)
		}
	}
	return out
}

// Find returns the post served for slug.
func (ix *Index) Find(slug string) (Doc, bool) {
	for _, d := range ix.byURL {
		if d.Kind == KindPost && d.Slug == slug {
			return d, true
		}
	}
	return Doc{}, false
}

func (ix *Index) hasSlug(slug string) bool {
	for _, d := range ix.Docs {
		if d.Kind == KindPost && d.Slug == slug {
			return true
		}
	}
	return false
}

// WritePost creates content/posts/<slug>.html.
// The posted date is written in the HTML head metadata.
func WritePost(root string, in Input) (Doc, error) {
	slug, when, err := normalize(in)
	if err != nil {
		return Doc{}, err
	}
	ix, err := Load(root)
	if err != nil {
		return Doc{}, err
	}
	if ix.hasSlug(slug) {
		return Doc{}, ErrExists
	}
	rel := postsDir + "/" + slug + ".html"
	doc := Doc{
		Rel:   rel,
		URL:   "/posts/" + slug,
		Slug:  slug,
		Title: in.Title,
		Date:  when,
		Dated: true,
		Draft: in.Draft,
		Body:  in.Body,
		Kind:  KindPost,
	}
	if err := save(root, &doc); err != nil {
		return Doc{}, err
	}
	return doc, nil
}

// UpdatePost rewrites the file that already has slug. The filename stays.
func UpdatePost(root, slug string, in Input) (Doc, error) {
	if strings.TrimSpace(in.Body) == "" {
		return Doc{}, ErrBody
	}
	if !slugRE.MatchString(slug) {
		return Doc{}, ErrSlug
	}
	when, err := stamp(in)
	if err != nil {
		return Doc{}, err
	}
	ix, err := Load(root)
	if err != nil {
		return Doc{}, err
	}
	doc, ok := ix.Find(slug)
	if !ok {
		return Doc{}, ErrNotFound
	}
	doc.Title = in.Title
	doc.Date = when
	doc.Dated = true
	doc.Draft = in.Draft
	doc.Body = in.Body
	if err := save(root, &doc); err != nil {
		return Doc{}, err
	}
	return doc, nil
}

// DeletePost removes every post file for slug.
// The returned paths are the files removed.
func DeletePost(root, slug string) ([]string, error) {
	if !slugRE.MatchString(slug) {
		return nil, ErrSlug
	}
	ix, err := Load(root)
	if err != nil {
		return nil, err
	}
	var rels []string
	for _, d := range ix.Docs {
		if d.Kind == KindPost && d.Slug == slug {
			rels = append(rels, d.Rel)
		}
	}
	if len(rels) == 0 {
		return nil, ErrNotFound
	}
	var removed []string
	var failed error
	for _, rel := range rels {
		full, err := safeJoin(root, rel)
		if err != nil {
			return removed, err
		}
		if err := os.Remove(full); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			failed = err
			continue
		}
		removed = append(removed, rel)
	}
	if len(removed) == 0 {
		if failed != nil {
			return nil, failed
		}
		return nil, ErrNotFound
	}
	return removed, failed
}

// winsURL reports whether doc replaces old for the same URL.
// Posts keep the newer date. Equal dates, and every other file, keep the
// lexicographically smaller path.
func winsURL(doc, old Doc) bool {
	if doc.Kind == KindPost && old.Kind == KindPost && !doc.Date.Equal(old.Date) {
		return doc.Date.After(old.Date)
	}
	return doc.Rel < old.Rel
}

// Slugify turns a title into a slug. It returns empty when nothing remains.
func Slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func stamp(in Input) (time.Time, error) {
	if strings.TrimSpace(in.Body) == "" {
		return time.Time{}, ErrBody
	}
	if in.Date.IsZero() {
		return time.Now().UTC(), nil
	}
	return in.Date.UTC(), nil
}

func normalize(in Input) (string, time.Time, error) {
	when, err := stamp(in)
	if err != nil {
		return "", time.Time{}, err
	}
	slug := in.Slug
	if slug == "" {
		slug = Slugify(in.Title)
	}
	if slug == "" {
		slug = "post-" + when.Format("20060102-150405")
	}
	if !slugRE.MatchString(slug) {
		return "", time.Time{}, ErrSlug
	}
	return slug, when, nil
}

func save(root string, doc *Doc) error {
	full, err := safeJoin(root, doc.Rel)
	if err != nil {
		return err
	}
	body, err := NormalizeHTML(doc.Body)
	if err != nil {
		return err
	}
	doc.Body = body
	data, err := encode(root, *doc)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Dir(full), full, data)
}

// writeAtomic replaces dest by renaming a finished temp file over it.
// The file and the directory are fsynced, so a crash leaves the previous file or the finished new one.
func writeAtomic(dir, dest string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := f.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, dest); err != nil {
		cleanup()
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	cerr := d.Close()
	if err != nil {
		return err
	}
	return cerr
}

func parseMarkdown(rel string, mod time.Time, raw []byte) (Doc, error) {
	fm, body, err := splitFront(raw)
	if err != nil {
		return Doc{}, err
	}
	var meta front
	if len(bytes.TrimSpace(fm)) > 0 {
		if err := yaml.Unmarshal(fm, &meta); err != nil {
			return Doc{}, err
		}
	}
	kind, urlPath, slug := classify(rel)
	doc := Doc{
		Rel:   rel,
		URL:   urlPath,
		Slug:  slug,
		Title: meta.Title,
		Draft: meta.Draft,
		Body:  body,
		Kind:  kind,
	}
	switch {
	case !meta.Date.IsZero():
		doc.Date = meta.Date.UTC()
		doc.Dated = true
	case kind == KindPost:
		doc.Date = mod.UTC()
		doc.Dated = true
	}
	return doc, nil
}

// classify names a post from its filename; dates live in document metadata.
func classify(rel string) (Kind, string, string) {
	stem := strings.TrimSuffix(strings.TrimSuffix(rel, ".md"), ".html")
	if rel == "index.md" || rel == "index.html" {
		return KindBlurb, "/", ""
	}
	base := path.Base(stem)
	if base == "index" {
		return KindPage, cleanURL("/" + path.Dir(rel)), ""
	}
	if path.Dir(rel) == postsDir {
		return KindPost, "/posts/" + base, base
	}
	return KindPage, cleanURL("/" + stem), ""
}

func cleanURL(u string) string {
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	u = path.Clean(u)
	if u == "." {
		return "/"
	}
	return u
}

func splitFront(raw []byte) ([]byte, string, error) {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, s, nil
	}
	rest := s[len("---\n"):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, s, nil
	}
	fm := rest[:idx]
	body := rest[idx+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	return []byte(fm), body, nil
}

func safeJoin(root, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", ErrEscape
	}
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) {
		return "", ErrEscape
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(rootAbs, rel)
	back, err := filepath.Rel(rootAbs, full)
	if err != nil {
		return "", err
	}
	if back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", ErrEscape
	}
	return full, nil
}
