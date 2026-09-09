package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var mangaCoverNames = []string{
	"folder.jpg",
	"folder.jpeg",
	"folder.png",
	"folder.webp",
	"cover.jpg",
	"cover.jpeg",
	"cover.png",
	"cover.webp",
}

func (s *Server) handleMangaCover(w http.ResponseWriter, r *http.Request) {
	series := strings.TrimSpace(r.URL.Query().Get("series"))
	if series == "" {
		http.NotFound(w, r)
		return
	}

	root := filepath.Clean(s.cfg.MangaDir)
	dir := filepath.Clean(filepath.Join(root, series))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	for _, name := range mangaCoverNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		http.ServeFile(w, r, path)
		return
	}
	http.NotFound(w, r)
}
