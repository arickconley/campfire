// Package web holds the embedded page templates.
package web

import "embed"

// FS is the embedded templates.
//
//go:embed templates
var FS embed.FS
