// Package site reads site.yaml from the content root.
package site

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the public site identity.
type Config struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	URL         string `yaml:"url"`
	// Style names a file in content/styles (paper) or a content-relative path (style.css).
	Style string `yaml:"style"`
}

// Load reads root/site.yaml. A missing file yields a default title.
func Load(root string) (Config, error) {
	cfg := Config{Title: "Site"}
	b, err := os.ReadFile(filepath.Join(root, "site.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = "Site"
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	cfg.Style = strings.TrimSpace(cfg.Style)
	return cfg, nil
}
