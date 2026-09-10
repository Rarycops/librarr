package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/JeremiahM37/librarr/internal/db"
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/releases"
)

// MangaWatchStats summarizes one watcher sync pass.
type MangaWatchStats struct {
	Watched   int `json:"watched"`
	Synced    int `json:"synced"`
	Queued    int `json:"queued"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
}

// MangaWatcher syncs official catalog releases and queues due items.
type MangaWatcher struct {
	db       *db.DB
	provider releases.CatalogProvider
	detector *SeriesDetector
	now      func() time.Time
}

// NewMangaWatcher wires catalog sync into the existing wanted scheduler.
func NewMangaWatcher(database *db.DB, provider releases.CatalogProvider, detector *SeriesDetector, now func() time.Time) *MangaWatcher {
	if now == nil {
		now = time.Now
	}
	return &MangaWatcher{
		db:       database,
		provider: provider,
		detector: detector,
		now:      now,
	}
}

// SyncAndQueue refreshes catalog rows and creates wanted entries for due releases.
func (w *MangaWatcher) SyncAndQueue(ctx context.Context) (MangaWatchStats, error) {
	stats := MangaWatchStats{}
	if w == nil || w.provider == nil || w.db == nil {
		return stats, nil
	}

	names, err := w.db.ListWatchEnabledSeries()
	if err != nil {
		return stats, err
	}
	stats.Watched = len(names)

	seriesList, err := w.detector.DetectSeries()
	if err != nil {
		return stats, err
	}
	seriesByName := map[string]SeriesInfo{}
	for _, info := range seriesList {
		seriesByName[strings.ToLower(info.SeriesName)] = info
	}

	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		info, ok := seriesByName[strings.ToLower(name)]
		if !ok {
			info = SeriesInfo{SeriesName: name}
		}

		author := ""
		syncedCount, syncErr := w.syncCatalog(ctx, name, author)
		if syncErr != nil {
			stats.Errors++
			slog.Warn("manga watcher catalog sync failed", "series", name, "error", syncErr)
			_ = w.db.SetSeriesCatalogSync(name, w.now(), syncErr.Error())
			continue
		}
		stats.Synced += syncedCount
		_ = w.db.SetSeriesCatalogSync(name, w.now(), "")

		queued, skipped, qErr := w.queueDueReleases(name, info)
		if qErr != nil {
			return stats, qErr
		}
		stats.Queued += queued
		stats.Skipped += skipped
	}
	return stats, nil
}

func (w *MangaWatcher) syncCatalog(ctx context.Context, seriesName, author string) (int, error) {
	releasesList, err := w.provider.SearchSeries(ctx, seriesName, author)
	if err != nil {
		return 0, err
	}
	for _, release := range releasesList {
		if err := w.db.UpsertMangaRelease(release); err != nil {
			return 0, err
		}
	}
	return len(releasesList), nil
}

func (w *MangaWatcher) queueDueReleases(seriesName string, info SeriesInfo) (queued, skipped int, err error) {
	enabled, mode, err := w.db.GetSeriesWatch(seriesName)
	if err != nil || !enabled {
		return 0, 0, err
	}

	detected, highest, err := w.db.GetSeriesDetection(seriesName)
	if err != nil {
		return 0, 0, err
	}
	if info.HighestOwnedUnit > highest {
		highest = info.HighestOwnedUnit
	}
	if info.DetectedReleaseKind != "" {
		detected = models.ReleaseKind(info.DetectedReleaseKind)
	}

	catalog, err := w.db.ListMangaReleases(seriesName, time.Time{})
	if err != nil {
		return 0, 0, err
	}

	now := w.now().UTC()
	for _, release := range catalog {
		if release.Sequence <= 0 {
			skipped++
			continue
		}
		if release.Sequence <= highest {
			skipped++
			continue
		}
		if release.OnSaleAt.After(now) {
			skipped++
			continue
		}
		if !releases.ReleaseModeAccepts(mode, detected, release.Kind) {
			skipped++
			continue
		}

		releaseKey := releases.ReleaseKey(release.Provider, release.ProviderKey)
		existing, err := w.db.FindWishlistByReleaseKey(releaseKey)
		if err != nil {
			return queued, skipped, err
		}
		if existing != nil {
			skipped++
			continue
		}

		_, err = w.db.AddWishlistItemWithOptions(models.WishlistItem{
			Title:      release.Title,
			Author:     release.Author,
			MediaType:  "manga",
			Monitored:  true,
			ReleaseKey: releaseKey,
			Source:     fmt.Sprintf("series-watcher:%s", release.Provider),
		})
		if err != nil {
			return queued, skipped, err
		}
		queued++
	}
	return queued, skipped, nil
}
