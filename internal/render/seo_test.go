package render

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestSEOImagesAndEscaping(t *testing.T) {
	for _, tc := range []struct{ name, head, body, image string }{
		{"relative", "", `<img src="../media/photo.jpg">`, "https://example.test/media/photo.jpg"},
		{"external", "", `<img src="https://images.example/photo.jpg?a=1&amp;b=2">`, "https://images.example/photo.jpg?a=1&b=2"},
		{"custom", `<meta property="og:image" content="/cover.jpg"><meta property="og:image:alt" content="Cover">`, `<img src="/body.jpg">`, "https://example.test/cover.jpg"},
		{"text only", "", `<p>No image</p>`, ""},
		{"unsafe", "", `<img src="javascript:alert(1)">`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := html.Parse(strings.NewReader("<html><head>" + tc.head + "</head><body></body></html>"))
			if err != nil {
				t.Fatal(err)
			}
			seo := SEO{URL: "https://example.test/posts/example", Title: `A "title" & <script>`, Description: "A & B", Type: "article", Body: tc.body}
			head := firstElement(tree, "head")
			seo.Apply(head)
			seo.Apply(head)
			counts := map[string]int{}
			for child := head.FirstChild; child != nil; child = child.NextSibling {
				property := attribute(child, "property")
				if property != "" {
					counts[property]++
				}
				if property == "og:image" && attribute(child, "content") != tc.image {
					t.Fatalf("wrong image: %s", attribute(child, "content"))
				}
				if property == "og:title" && attribute(child, "content") != seo.Title {
					t.Fatal("title changed")
				}
			}
			want := 0
			if tc.image != "" {
				want = 1
			}
			if counts["og:image"] != want || counts["og:title"] != 1 {
				t.Fatalf("duplicate/missing metadata: %v", counts)
			}
			if tc.name == "custom" && counts["og:image:alt"] != 1 {
				t.Fatal("lost custom image alt")
			}
			var output strings.Builder
			if err := html.Render(&output, tree); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "<script>") {
				t.Fatal("unescaped title")
			}
		})
	}
}
