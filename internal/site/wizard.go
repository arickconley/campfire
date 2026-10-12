package site

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrTitle = errors.New("title is required")
	ErrStyle = errors.New("invalid style")
)

// Exists reports whether root/site.yaml is present.
func Exists(root string) bool {
	_, err := os.Stat(filepath.Join(root, "site.yaml"))
	return err == nil
}

// Write creates root/site.yaml. Title is required.
func Write(root string, cfg Config) error {
	cfg.Title = strings.TrimSpace(cfg.Title)
	cfg.Description = strings.TrimSpace(cfg.Description)
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	cfg.Style = strings.TrimSpace(cfg.Style)
	if cfg.Title == "" {
		return ErrTitle
	}
	if cfg.Style == "" {
		cfg.Style = "paper"
	}
	if !validStyle(cfg.Style) {
		return ErrStyle
	}
	body, err := yaml.Marshal(wizardFile{
		Title:       cfg.Title,
		Description: cfg.Description,
		URL:         cfg.URL,
		Style:       cfg.Style,
	})
	if err != nil {
		return err
	}
	return writeAtomic(root, "site.yaml", body, 0o644)
}

// GenerateToken returns a new write-API bearer token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SaveToken writes root/.token for the write API. The file is not served.
func SaveToken(root, token string) error {
	return writeAtomic(root, ".token", []byte(token+"\n"), 0o600)
}

// writeAtomic writes body to name in dir by renaming a finished temp file over it.
// The file and the directory are fsynced, so a crash leaves the previous file or the finished new one.
func writeAtomic(dir, name string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+strings.TrimPrefix(name, ".")+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	remove := func() { os.Remove(tmp) }
	if _, err := f.Write(body); err != nil {
		f.Close()
		remove()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		remove()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		remove()
		return err
	}
	if err := f.Close(); err != nil {
		remove()
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		remove()
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

// LoadToken reads root/.token. A missing file is an empty token.
func LoadToken(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, ".token"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Styles lists names from content/styles/*.css.
// An empty folder yields paper, ink, and plain.
func Styles(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "styles"))
	if err != nil {
		return []string{"paper", "ink", "plain"}
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".css")
		if validStyle(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []string{"paper", "ink", "plain"}
	}
	return names
}

// DefaultTitle is the suggested site title for a content directory.
func DefaultTitle(dir string) string {
	base := filepath.Base(dir)
	switch base {
	case "", ".", "..", string(filepath.Separator), "content":
		return "Site"
	default:
		return base
	}
}

type wizardFile struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`
	URL         string `yaml:"url,omitempty"`
	Style       string `yaml:"style,omitempty"`
}

func validStyle(s string) bool {
	if s == "" || strings.ContainsRune(s, 0) || filepath.IsAbs(s) {
		return false
	}
	clean := filepath.Clean(s)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
