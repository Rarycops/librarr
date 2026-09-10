package api

import (
	"context"
	"encoding/csv"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleCSVImport(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form for file upload.
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Failed to parse form: " + err.Error(),
		})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "No file uploaded",
		})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // variable columns

	// Read header.
	header, err := reader.Read()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Failed to read CSV header",
		})
		return
	}

	// Find column indices.
	colMap := make(map[string]int)
	for i, h := range header {
		colMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	titleIdx, hasTitleCol := colMap["title"]
	authorIdx := colMap["author"]
	mediaTypeIdx := colMap["media_type"]
	_ = authorIdx
	_ = mediaTypeIdx

	if !hasTitleCol {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "CSV must have a 'title' column",
		})
		return
	}

	var queued int
	var errors []string

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors = append(errors, "CSV read error: "+err.Error())
			continue
		}

		if titleIdx >= len(record) {
			continue
		}
		title := strings.TrimSpace(record[titleIdx])
		if title == "" {
			continue
		}

		author := ""
		if idx, ok := colMap["author"]; ok && idx < len(record) {
			author = strings.TrimSpace(record[idx])
		}

		mediaType := "ebook"
		if idx, ok := colMap["media_type"]; ok && idx < len(record) {
			mt := strings.TrimSpace(strings.ToLower(record[idx]))
			if mt == "audiobook" || mt == "manga" {
				mediaType = mt
			}
		}

		// Search all sources and queue best result.
		// Use background context since the goroutine outlives the HTTP request.
		go func(title, author, mediaType string) {
			tab := "main"
			if mediaType == "audiobook" {
				tab = "audiobook"
			} else if mediaType == "manga" {
				tab = "manga"
			}

			query := title
			if author != "" {
				query = title + " " + author
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			results, _ := s.searchMgr.Search(ctx, tab, query)
			if len(results) == 0 {
				slog.Warn("CSV import: no results", "title", title)
				return
			}

			best := results[0]

			// Queue download.
			if best.MD5 != "" {
				s.downloadMgr.StartAnnasDownloadForMediaType(best.MD5, title, mediaType, 0)
			} else if best.DownloadProtocol == "torrent" || best.MagnetURL != "" || best.InfoHash != "" {
				url := best.MagnetURL
				if url == "" {
					url = best.DownloadURL
				}
				if url == "" {
					url = "magnet:?xt=urn:btih:" + best.InfoHash
				}
				savePath, category := s.cfg.QBSavePath, s.cfg.QBCategory
				switch mediaType {
				case "audiobook":
					savePath, category = s.cfg.QBAudiobookSavePath, s.cfg.QBAudiobookCategory
				case "manga":
					savePath, category = s.cfg.QBMangaSavePath, s.cfg.QBMangaCategory
				}
				s.downloadMgr.StartTorrentDownload(url, title, savePath, category, best.InfoHash)
			} else if best.DownloadURL != "" || best.EpubURL != "" {
				dlURL := best.DownloadURL
				if dlURL == "" {
					dlURL = best.EpubURL
				}
				s.downloadMgr.StartDirectDownloadForMediaType(
					dlURL, title, best.Source, best.SourceID, best.Author, mediaType, 0,
				)
			} else {
				slog.Warn("CSV import: no downloadable result", "title", title)
			}
		}(title, author, mediaType)

		queued++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"queued":  queued,
		"errors":  errors,
	})
}
