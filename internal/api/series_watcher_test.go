package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/JeremiahM37/librarr/internal/config"
	"github.com/JeremiahM37/librarr/internal/db"
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/scheduler"
	"github.com/JeremiahM37/librarr/internal/search"
)

func seriesWatcherServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{MangaDir: "/books/manga"}
	health := search.NewHealthTracker(3, 300)
	searchMgr := search.NewManager(cfg, nil, health)
	s := &Server{
		cfg:            cfg,
		db:             database,
		searchMgr:      searchMgr,
		seriesDetector: nil,
	}
	return s, database
}

func TestSeriesWatcherPatchCreatesRow(t *testing.T) {
	s, database := seriesWatcherServer(t)
	body, _ := json.Marshal(map[string]interface{}{
		"enabled":      true,
		"release_mode": "volume",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/series/Witch+Hat+Atelier/watch", bytes.NewReader(body))
	req.SetPathValue("name", "Witch Hat Atelier")
	req = req.WithContext(context.WithValue(req.Context(), ctxUsername, "admin"))
	rr := httptest.NewRecorder()
	s.handleSeriesWatch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	enabled, mode, err := database.GetSeriesWatch("Witch Hat Atelier")
	if err != nil || !enabled || mode != models.ReleaseModeVolume {
		t.Fatalf("watch state = %t, %q, %v", enabled, mode, err)
	}
}

func TestSeriesWatcherPatchRejectsInvalidMode(t *testing.T) {
	s, _ := seriesWatcherServer(t)
	body, _ := json.Marshal(map[string]interface{}{
		"enabled":      true,
		"release_mode": "weekly",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/series/Test/watch", bytes.NewReader(body))
	req.SetPathValue("name", "Test")
	req = req.WithContext(context.WithValue(req.Context(), ctxUsername, "admin"))
	rr := httptest.NewRecorder()
	s.handleSeriesWatch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestSeriesWatcherListIncludesFutureRelease(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.UpsertMangaRelease(models.MangaRelease{
		Provider: "prh", ProviderKey: "9798888779781", SeriesName: "Witch Hat Atelier",
		Title: "Witch Hat Atelier 15", Kind: models.ReleaseKindVolume, Sequence: 15,
		OnSaleAt: time.Date(2026, 12, 8, 0, 0, 0, 0, time.UTC),
		FetchedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateSeriesDetection("Witch Hat Atelier", models.ReleaseKindVolume, 14); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{MangaDir: "/books/manga"}
	health := search.NewHealthTracker(3, 300)
	searchMgr := search.NewManager(cfg, nil, health)
	detector := scheduler.NewSeriesDetector(database, searchMgr, nil, cfg.MangaDir)
	s := &Server{cfg: cfg, db: database, searchMgr: searchMgr, seriesDetector: detector}

	req := httptest.NewRequest(http.MethodGet, "/api/series", nil)
	rr := httptest.NewRecorder()
	s.handleListSeries(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Series []map[string]interface{} `json:"series"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
}
