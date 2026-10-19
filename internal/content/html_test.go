package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLNormalization(t *testing.T) {
	source := `<p>Fish &amp; chips<!-- invisible -- --><br>line<img src="/pic?a=1&amp;b=2" alt="" title="Picture"></p><ol start="3"><li>Third</li></ol><ul><li><input checked disabled type="checkbox">Done</li></ul>`
	body, err := NormalizeHTML(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<br/>`) || !strings.Contains(body, `start="3"`) || strings.Contains(body, "<!--") || !strings.Contains(body, `type="checkbox"`) {
		t.Fatal(body)
	}
	again, err := NormalizeHTML(body)
	if err != nil || again != body {
		t.Fatalf("normalization is not stable: %v %s", err, again)
	}
	for _, invalid := range []string{`<input type="text">`, `<input type="checkbox">`, `<a href="java&#9;script:alert(1)">bad</a>`, "<p>invalid\x01text</p>"} {
		if _, err := NormalizeHTML(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestHTMLMigrationKeepsConflicts(t *testing.T) {
	for _, existing := range []string{"posts/old.html", ".markdown-backup/posts/old.md"} {
		t.Run(existing, func(t *testing.T) {
			root := t.TempDir()
			source := "---\ntitle: Old\n---\nOriginal **content**.\n"
			if err := os.MkdirAll(filepath.Join(root, "posts"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "posts/old.md"), []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, existing)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest, []byte("Keep this file"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(root); err == nil {
				t.Fatal("migration overwrote a conflict")
			}
			original, err := os.ReadFile(filepath.Join(root, "posts/old.md"))
			if err != nil || string(original) != source {
				t.Fatal("original Markdown changed")
			}
			kept, err := os.ReadFile(dest)
			if err != nil || string(kept) != "Keep this file" {
				t.Fatal("existing destination changed")
			}
		})
	}
}
