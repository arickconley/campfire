package server

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ix, cfg, ok := s.snap(w)
	if !ok {
		return
	}
	if cfg.PublicURL("/") == "" {
		http.Error(w, "Configure a public HTTP(S) url in site.yaml to enable the sitemap.", http.StatusServiceUnavailable)
		return
	}
	type entry struct {
		Location string `xml:"loc"`
	}
	data := struct {
		XMLName xml.Name `xml:"urlset"`
		NS      string   `xml:"xmlns,attr"`
		URLs    []entry  `xml:"url"`
	}{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: []entry{{cfg.PublicURL("/")}}}
	for _, doc := range append(ix.Pages(), ix.Feed()...) {
		data.URLs = append(data.URLs, entry{cfg.PublicURL(doc.URL)})
	}
	// shortcut: one sitemap for small blogs; add sitemap indexes above 50,000 URLs.
	if len(data.URLs) > 50_000 {
		http.Error(w, "Site exceeds the single-sitemap URL limit.", http.StatusInternalServerError)
		return
	}
	body, err := xml.MarshalIndent(data, "", "  ")
	if err != nil {
		s.htmlErr(w, err)
		return
	}
	if len(body)+len(xml.Header) > 50*1024*1024 {
		http.Error(w, "Site exceeds the single-sitemap size limit.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodHead {
		fmt.Fprint(w, xml.Header, string(body))
	}
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	ix, cfg, ok := s.snap(w)
	if !ok {
		return
	}
	body := "User-agent: *\nAllow: /\n"
	// Keep site-owner crawler rules; the content index excludes hidden files and symlinks.
	if rel, found := ix.Static("/robots.txt"); found {
		raw, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(rel)))
		if err != nil {
			s.htmlErr(w, err)
			return
		}
		body = string(raw)
	}
	if sitemap := cfg.PublicURL("/sitemap.xml"); sitemap != "" {
		found := false
		for _, line := range strings.Split(body, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(key), "sitemap") && strings.TrimSpace(value) == sitemap {
				found = true
			}
		}
		if !found {
			body = strings.TrimRight(body, "\r\n") + "\n\nSitemap: " + sitemap + "\n"
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodHead {
		fmt.Fprint(w, body)
	}
}
