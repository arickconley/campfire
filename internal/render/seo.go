package render

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// SEO is generated from saved content and the configured public site identity.
type SEO struct {
	URL, Title, Description, SiteName, Type, Body string
}

func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func firstElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := firstElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

// Apply replaces managed head tags even in existing custom templates.
func (s SEO) Apply(head *html.Node) {
	image := ""
	var imageNode *html.Node
	for child := head.FirstChild; child != nil; {
		next := child.NextSibling
		name := strings.ToLower(attribute(child, "name"))
		property := strings.ToLower(attribute(child, "property"))
		if property == "" {
			property = name
		}
		if child.Type == html.ElementNode && child.Data == "meta" {
			if property == "og:image" && image == "" {
				image = attribute(child, "content")
				imageNode = child
			}
			switch property {
			case "og:title", "og:description", "og:site_name", "og:type", "og:url":
				head.RemoveChild(child)
			default:
				if name == "description" {
					head.RemoveChild(child)
				}
			}
		} else if child.Type == html.ElementNode && child.Data == "link" {
			for _, rel := range strings.Fields(strings.ToLower(attribute(child, "rel"))) {
				if rel == "canonical" {
					head.RemoveChild(child)
					break
				}
			}
		}
		child = next
	}
	add := func(key, name, value string) {
		if value != "" {
			head.AppendChild(&html.Node{Type: html.ElementNode, Data: "meta", DataAtom: atom.Meta, Attr: []html.Attribute{{Key: key, Val: name}, {Key: "content", Val: value}}})
		}
	}
	add("name", "description", s.Description)
	add("property", "og:title", s.Title)
	add("property", "og:description", s.Description)
	add("property", "og:site_name", s.SiteName)
	add("property", "og:type", s.Type)
	if s.URL != "" {
		head.AppendChild(&html.Node{Type: html.ElementNode, Data: "link", DataAtom: atom.Link, Attr: []html.Attribute{{Key: "rel", Val: "canonical"}, {Key: "href", Val: s.URL}}})
		add("property", "og:url", s.URL)
	}
	if image == "" {
		tree, _ := html.Parse(strings.NewReader(s.Body))
		if tree != nil {
			if img := firstElement(tree, "img"); img != nil {
				image = attribute(img, "src")
			}
		}
	}
	if image != "" {
		ref, err := url.Parse(image)
		if err == nil {
			if base, err := url.Parse(s.URL); err == nil && s.URL != "" {
				ref = base.ResolveReference(ref)
			}
			if (ref.Scheme == "https" || ref.Scheme == "http") && ref.Hostname() != "" && ref.User == nil {
				if imageNode == nil {
					add("property", "og:image", ref.String())
				} else {
					for i := range imageNode.Attr {
						if imageNode.Attr[i].Key == "content" {
							imageNode.Attr[i].Val = ref.String()
						}
					}
				}
				return
			}
		}
	}
	if imageNode != nil {
		head.RemoveChild(imageNode)
	}
}
