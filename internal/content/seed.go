package content

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Seed writes the starter tree when root is missing or has no visible files.
// site.yaml and .token stay absent so the setup page can create them.
// A directory that already has a visible file is left alone.
func Seed(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("content directory is empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return seedNew(root)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	has, err := hasVisible(root)
	if err != nil || has {
		return err
	}
	if err := writeDefaults(root); err != nil {
		return err
	}
	log.Printf("created content directory %s", root)
	return nil
}

func seedNew(root string) error {
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := writeDefaults(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, root); err != nil {
		if _, statErr := os.Stat(root); statErr == nil {
			return nil
		}
		return err
	}
	if err := os.Chmod(root, 0o755); err != nil {
		return err
	}
	log.Printf("created content directory %s", root)
	return nil
}

func hasVisible(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return true, nil
		}
	}
	return false, nil
}

func writeDefaults(root string) error {
	return fs.WalkDir(defaultFS, "defaults", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(name, "defaults")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		b, err := defaultFS.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0o644)
	})
}
