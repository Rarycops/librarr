package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JeremiahM37/librarr/internal/db"
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/releases"
	"github.com/JeremiahM37/librarr/internal/search"
	"github.com/JeremiahM37/librarr/internal/webhook"
)

// SeriesInfo holds detected series data.
type SeriesInfo struct {
	ID                  int64                `json:"id"`
	SeriesName          string               `json:"series_name"`
	KnownTotal          int                  `json:"known_total"`
	OwnedCount          int                  `json:"owned_count"`
	OwnedBooks          []string             `json:"owned_books,omitempty"`
	MissingBooks        []string             `json:"missing_books,omitempty"`
	LastChecked         time.Time            `json:"last_checked"`
	Manga               bool                 `json:"manga,omitempty"`
	WatchEnabled        bool                 `json:"watch_enabled"`
	ReleaseMode         string               `json:"release_mode"`
	DetectedReleaseKind string               `json:"detected_release_kind"`
	HighestOwnedUnit    float64              `json:"highest_owned_unit"`
	NextRelease         *models.MangaRelease `json:"next_release,omitempty"`
	CatalogStale        bool                 `json:"catalog_stale"`
	CatalogError        string               `json:"catalog_error,omitempty"`
}

// SeriesDetector analyzes the library for series patterns.
type SeriesDetector struct {
	db            *db.DB
	searchMgr     *search.Manager
	webhookSender *webhook.Sender
	httpClient    *http.Client
	mangaRoot     string
}

// NewSeriesDetector creates a new series detector.
func NewSeriesDetector(database *db.DB, searchMgr *search.Manager, ws *webhook.Sender, mangaRoot string) *SeriesDetector {
	return &SeriesDetector{
		db:            database,
		searchMgr:     searchMgr,
		webhookSender: ws,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		mangaRoot:     mangaRoot,
	}
}

// Common patterns for detecting series in titles.
var seriesPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.+?)[\s,:-]+(?:book|vol\.?|volume|#)\s*(\d+)`),
	regexp.MustCompile(`(?i)^(.+?)\s*\((\d+)\)`),
	regexp.MustCompile(`(?i)^(.+?)\s+(\d+)$`),
}

var mangaVolumePattern = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:volume|vol|v)\s*0*(\d+)(?:\b|[^\d])`)

func (d *SeriesDetector) mangaVolume(item models.LibraryItem) (string, int, bool) {
	if item.MediaType != "manga" || d.mangaRoot == "" {
		return "", 0, false
	}
	relative, err := filepath.Rel(d.mangaRoot, item.FilePath)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return "", 0, false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 2 {
		return "", 0, false
	}
	series := parts[0]
	for _, part := range parts[1:] {
		if match := mangaVolumePattern.FindStringSubmatch(part); len(match) == 2 {
			number, err := strconv.Atoi(match[1])
			if err == nil && number > 0 {
				return series, number, true
			}
		}
	}
	return "", 0, false
}

// DetectedSeries represents a series found in the library.
type DetectedSeries struct {
	Name       string
	OwnedBooks map[int]string // book number -> title
	Manga      bool
}

// DetectSeries scans the library for series patterns and returns detected series.
func (d *SeriesDetector) DetectSeries() ([]SeriesInfo, error) {
	items, err := d.db.GetItems("", 100000, 0)
	if err != nil {
		return nil, err
	}

	seriesMap := make(map[string]*DetectedSeries)
	mangaItems := make(map[string][]models.LibraryItem)

	for _, item := range items {
		if seriesName, volume, ok := d.mangaVolume(item); ok {
			key := strings.ToLower(seriesName)
			if _, exists := seriesMap[key]; !exists {
				seriesMap[key] = &DetectedSeries{
					Name:       seriesName,
					OwnedBooks: make(map[int]string),
					Manga:      true,
				}
			}
			seriesMap[key].Manga = true
			seriesMap[key].OwnedBooks[volume] = item.Title
			mangaItems[key] = append(mangaItems[key], item)
			continue
		}
		for _, pat := range seriesPatterns {
			matches := pat.FindStringSubmatch(item.Title)
			if len(matches) >= 3 {
				seriesName := strings.TrimSpace(matches[1])
				bookNum, err := strconv.Atoi(matches[2])
				if err != nil || bookNum < 1 || bookNum > 100 {
					continue
				}

				key := strings.ToLower(seriesName)
				if _, ok := seriesMap[key]; !ok {
					seriesMap[key] = &DetectedSeries{
						Name:       seriesName,
						OwnedBooks: make(map[int]string),
					}
				}
				seriesMap[key].OwnedBooks[bookNum] = item.Title
				break
			}
		}
	}

	var result []SeriesInfo
	for key, series := range seriesMap {
		if series.Manga {
			info := d.buildMangaSeriesInfo(series, mangaItems[key])
			info = d.enrichSeriesInfo(info)
			id, _ := d.db.UpsertSeriesTracking(info.SeriesName, info.KnownTotal, info.OwnedCount)
			info.ID = id
			_ = d.db.UpdateSeriesDetection(info.SeriesName, models.ReleaseKind(info.DetectedReleaseKind), info.HighestOwnedUnit)
			result = append(result, info)
			continue
		}
		if len(series.OwnedBooks) < 2 {
			continue
		}

		maxNum := 0
		for num := range series.OwnedBooks {
			if num > maxNum {
				maxNum = num
			}
		}

		total := maxNum
		olTotal := d.getOpenLibrarySeriesTotal(series.Name)
		if olTotal > total {
			total = olTotal
		}

		var owned []string
		var missing []string
		omnibus := strings.Contains(strings.ToLower(series.Name), "omnibus")
		for i := 1; i <= total; i++ {
			if title, ok := series.OwnedBooks[i]; ok {
				owned = append(owned, title)
			} else if omnibus && i%2 == 0 {
				continue
			} else {
				missing = append(missing, fmt.Sprintf("%s Book %d", series.Name, i))
			}
		}

		info := SeriesInfo{
			SeriesName:   series.Name,
			KnownTotal:   total,
			OwnedCount:   len(series.OwnedBooks),
			OwnedBooks:   owned,
			MissingBooks: missing,
			LastChecked:  time.Now(),
		}
		id, _ := d.db.UpsertSeriesTracking(info.SeriesName, info.KnownTotal, info.OwnedCount)
		info.ID = id
		result = append(result, info)
	}

	return result, nil
}

func (d *SeriesDetector) buildMangaSeriesInfo(series *DetectedSeries, items []models.LibraryItem) SeriesInfo {
	maxNum := 0
	for num := range series.OwnedBooks {
		if num > maxNum {
			maxNum = num
		}
	}

	var owned []string
	var missing []string
	for i := 1; i <= maxNum; i++ {
		if title, ok := series.OwnedBooks[i]; ok {
			owned = append(owned, title)
		} else {
			missing = append(missing, fmt.Sprintf("%s Volume %d", series.Name, i))
		}
	}

	detected := releases.DetectDominantReleaseKind(items)
	highest := highestOwnedUnit(items)

	return SeriesInfo{
		SeriesName:          series.Name,
		KnownTotal:          maxNum,
		OwnedCount:          len(series.OwnedBooks),
		OwnedBooks:          owned,
		MissingBooks:        missing,
		LastChecked:         time.Now(),
		Manga:               true,
		DetectedReleaseKind: string(detected),
		HighestOwnedUnit:    highest,
	}
}

func highestOwnedUnit(items []models.LibraryItem) float64 {
	var highest float64
	for _, item := range items {
		if kind, seq, ok := releases.ParseMangaReleaseText(filepath.Base(item.FilePath)); ok && kind != models.ReleaseKindUnknown {
			if seq > highest {
				highest = seq
			}
		}
	}
	return highest
}

func (d *SeriesDetector) enrichSeriesInfo(info SeriesInfo) SeriesInfo {
	enabled, mode, err := d.db.GetSeriesWatch(info.SeriesName)
	if err == nil {
		info.WatchEnabled = enabled
		if mode != "" {
			info.ReleaseMode = string(mode)
		}
	}
	if info.ReleaseMode == "" {
		info.ReleaseMode = string(models.ReleaseModeAuto)
	}

	tracking, err := d.db.GetSeriesTracking()
	if err == nil {
		for _, row := range tracking {
			if !strings.EqualFold(fmt.Sprint(row["series_name"]), info.SeriesName) {
				continue
			}
			if info.DetectedReleaseKind == "" {
				info.DetectedReleaseKind = fmt.Sprint(row["detected_release_kind"])
			}
			if info.HighestOwnedUnit == 0 {
				if v, ok := row["highest_owned_unit"].(float64); ok {
					info.HighestOwnedUnit = v
				}
			}
			info.CatalogError = fmt.Sprint(row["catalog_error"])
			if info.CatalogError == "<nil>" {
				info.CatalogError = ""
			}
			syncRaw := fmt.Sprint(row["catalog_last_sync"])
			if enabled {
				syncAt, parseErr := time.Parse(time.RFC3339, syncRaw)
				if parseErr != nil || syncAt.IsZero() || time.Since(syncAt) > 7*24*time.Hour {
					info.CatalogStale = true
				}
			}
			break
		}
	}

	releasesList, err := d.db.ListMangaReleases(info.SeriesName, time.Time{})
	if err == nil {
		info.NextRelease = pickNextCatalogRelease(releasesList, info)
	}
	return info
}

func pickNextCatalogRelease(catalog []models.MangaRelease, info SeriesInfo) *models.MangaRelease {
	detected := models.ReleaseKind(info.DetectedReleaseKind)
	mode := models.ReleaseMode(info.ReleaseMode)
	if mode == "" {
		mode = models.ReleaseModeAuto
	}

	var best *models.MangaRelease
	for i := range catalog {
		release := catalog[i]
		if release.Sequence <= 0 {
			continue
		}
		if release.Sequence <= info.HighestOwnedUnit {
			continue
		}
		if !releases.ReleaseModeAccepts(mode, detected, release.Kind) {
			continue
		}
		if best == nil || release.OnSaleAt.Before(best.OnSaleAt) ||
			(release.OnSaleAt.Equal(best.OnSaleAt) && release.Sequence < best.Sequence) {
			copy := release
			best = &copy
		}
	}
	return best
}

// GetMissing returns missing books for a named series.
func (d *SeriesDetector) GetMissing(seriesName string) ([]string, error) {
	allSeries, err := d.DetectSeries()
	if err != nil {
		return nil, err
	}

	for _, s := range allSeries {
		if strings.EqualFold(s.SeriesName, seriesName) {
			return s.MissingBooks, nil
		}
	}

	return nil, fmt.Errorf("series not found: %s", seriesName)
}

// SearchMissing searches for missing books in a series.
func (d *SeriesDetector) SearchMissing(seriesName string) ([]models.SearchResult, error) {
	missing, err := d.GetMissing(seriesName)
	if err != nil {
		return nil, err
	}

	var allResults []models.SearchResult
	for _, title := range missing {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		results, _ := d.searchMgr.Search(ctx, "main", title)
		cancel()

		if len(results) > 0 {
			// Take only the best result for each missing book.
			best := results[0]
			if best.Score >= 50 {
				allResults = append(allResults, best)
			}
		}

		time.Sleep(3 * time.Second)
	}

	// Send webhook if missing books found.
	if d.webhookSender != nil && len(allResults) > 0 {
		d.webhookSender.Send(webhook.Payload{
			Event:   webhook.EventSeriesMissing,
			Title:   "Series: " + seriesName,
			Message: fmt.Sprintf("Found %d searchable missing books", len(allResults)),
			Status:  "info",
		})
	}

	return allResults, nil
}

// getOpenLibrarySeriesTotal tries to find total book count from Open Library.
func (d *SeriesDetector) getOpenLibrarySeriesTotal(seriesName string) int {
	u := fmt.Sprintf("https://openlibrary.org/search.json?q=%s&limit=5", url.QueryEscape(seriesName))
	req, err := http.NewRequestWithContext(context.Background(), "GET", u, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "Librarr/2.0")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0
	}

	var data struct {
		NumFound int `json:"numFound"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0
	}

	// Cap at reasonable numbers.
	if data.NumFound > 50 {
		return 0 // Too many results, not a useful series count
	}
	return data.NumFound
}

// ScanForScheduler is called by the scheduler to check all tracked series for missing books.
func (d *SeriesDetector) ScanForScheduler() {
	series, err := d.DetectSeries()
	if err != nil {
		slog.Error("series scan failed", "error", err)
		return
	}

	for _, s := range series {
		if len(s.MissingBooks) > 0 {
			slog.Info("series incomplete",
				"series", s.SeriesName,
				"owned", s.OwnedCount,
				"total", s.KnownTotal,
				"missing", len(s.MissingBooks),
			)
		}
	}
}
