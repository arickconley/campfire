package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"campfire/internal/render"
)

// Migrate converts legacy Markdown once and archives each original in a hidden
// backup directory only after the complete HTML replacement has been written.
func Migrate(root string) error {
	ix, err := load(root, true)
	if err != nil {
		return err
	}
	for _, doc := range ix.Docs {
		original := doc.Rel
		doc.Rel = strings.TrimSuffix(original, ".md") + ".html"
		dest, err := safeJoin(root, doc.Rel)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("migration would overwrite %s", doc.Rel)
		} else if !os.IsNotExist(err) {
			return err
		}
		backup, err := safeJoin(root, ".markdown-backup/"+original)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			return err
		}
		if _, err := os.Lstat(backup); err == nil {
			return fmt.Errorf("backup already exists: %s", original)
		} else if !os.IsNotExist(err) {
			return err
		}
		doc.Body, err = render.LegacyMarkdown(doc.Body)
		if err != nil {
			return err
		}
		if err := save(root, &doc); err != nil {
			return fmt.Errorf("convert %s: %w", original, err)
		}
		source, err := safeJoin(root, original)
		if err != nil {
			return err
		}
		if err := os.Rename(source, backup); err != nil {
			return err
		}
		if err := syncDir(filepath.Dir(backup)); err != nil {
			return err
		}
		if err := syncDir(filepath.Dir(source)); err != nil {
			return err
		}
	}
	return Rebuild(root)
}

// Rebuild reapplies the current shared templates and site settings to saved pages.
func Rebuild(root string) error {
	ix, err := Load(root)
	if err != nil {
		return err
	}
	for _, doc := range ix.Docs {
		if err := save(root, &doc); err != nil {
			return fmt.Errorf("rebuild %s: %w", doc.Rel, err)
		}
	}
	return nil
}

func HasMarkdown(root string) (bool, error) {
	ix, err := load(root, true)
	if err != nil {
		return false, err
	}
	return len(ix.Docs) > 0, nil
}
