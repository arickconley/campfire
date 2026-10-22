package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"campfire/internal/content"
)

func (s *Server) siteFiles(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	files, err := content.SiteFiles(s.Root)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) siteFile(w http.ResponseWriter, r *http.Request) {
	if !s.auth(w, r) {
		return
	}
	rel := r.URL.Query().Get("path")
	if r.Method == http.MethodGet {
		s.mu.RLock()
		defer s.mu.RUnlock()
		file, err := content.ReadSiteFile(s.Root, rel)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, file)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in content.SiteFile
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid json")
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := content.UpdateSiteFile(s.Root, rel, in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}
