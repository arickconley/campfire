package render

import (
	"bytes"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// LegacyMarkdown is used only by the explicit Markdown migration command.
func LegacyMarkdown(src string) (string, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(escapeHTML{}, 1000))))
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// escapeHTML turns raw HTML nodes into text so the renderer escapes them.
type escapeHTML struct{}

func (escapeHTML) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var nodes []ast.Node
	var vals [][]byte
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var raw []byte
		switch x := n.(type) {
		case *ast.HTMLBlock:
			raw = x.Text(src)
		case *ast.RawHTML:
			raw = x.Text(src)
		default:
			return ast.WalkContinue, nil
		}
		nodes = append(nodes, n)
		vals = append(vals, append([]byte(nil), raw...))
		return ast.WalkSkipChildren, nil
	})
	for i, n := range nodes {
		p := n.Parent()
		p.InsertBefore(p, n, ast.NewString(vals[i]))
		p.RemoveChild(p, n)
	}
}
