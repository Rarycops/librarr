package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/JeremiahM37/librarr/internal/db"
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/releases"
)

type fakeCatalogProvider struct {
	data map[string][]models.MangaRelease
	err  error
}

func (f fakeCatalogProvider) Name() string { return "fake" }

func (f fakeCatalogProvider) SearchSeries(_ context.Context, seriesName, _ string) ([]models.MangaRelease, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data[seriesName], nil
}

func TestMangaWatcherQueuesDueRelease(t *testing.T) {
	d := dbNewTestDB(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	provider := fakeCatalogProvider{data: map[string][]models.MangaRelease{
		"Vinland Saga": {{
			Provider: "prh", ProviderKey: "9798888779347", SeriesName: "Vinland Saga",
			Title: "Vinland Saga 15", Kind: models.ReleaseKindVolume, Sequence: 15,
			OnSaleAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), Final: true,
		}},
		"Witch Hat Atelier": {{
			Provider: "prh", ProviderKey: "9798888779781", SeriesName: "Witch Hat Atelier",
			Title: "Witch Hat Atelier 15", Kind: models.ReleaseKindVolume, Sequence: 15,
			OnSaleAt: time.Date(2026, 12, 8, 0, 0, 0, 0, time.UTC),
		}},
	}}
	detector := &SeriesDetector{db: d, mangaRoot: "/books/manga"}
	watcher := NewMangaWatcher(d, provider, detector, func() time.Time { return now })

	if err := d.SetSeriesWatch("Vinland Saga", true, models.ReleaseModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSeriesWatch("Witch Hat Atelier", true, models.ReleaseModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateSeriesDetection("Vinland Saga", models.ReleaseKindVolume, 14); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateSeriesDetection("Witch Hat Atelier", models.ReleaseKindVolume, 14); err != nil {
		t.Fatal(err)
	}

	stats, err := watcher.SyncAndQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Queued != 1 {
		t.Fatalf("queued = %d, want 1", stats.Queued)
	}
	item, err := d.FindWishlistByReleaseKey(releases.ReleaseKey("prh", "9798888779347"))
	if err != nil || item == nil {
		t.Fatalf("vinland wishlist = %#v, %v", item, err)
	}
	if _, err := d.FindWishlistByReleaseKey(releases.ReleaseKey("prh", "9798888779781")); err != nil {
		t.Fatal(err)
	} else if item, _ := d.FindWishlistByReleaseKey(releases.ReleaseKey("prh", "9798888779781")); item != nil {
		t.Fatal("future witch hat release should not be queued")
	}

	stats, err = watcher.SyncAndQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Queued != 0 {
		t.Fatalf("second run queued = %d, want 0", stats.Queued)
	}
}

func TestMangaWatcherIgnoresDisabledWatcher(t *testing.T) {
	d := dbNewTestDB(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	provider := fakeCatalogProvider{data: map[string][]models.MangaRelease{
		"Vinland Saga": {{
			Provider: "prh", ProviderKey: "9798888779347", SeriesName: "Vinland Saga",
			Title: "Vinland Saga 15", Kind: models.ReleaseKindVolume, Sequence: 15,
			OnSaleAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		}},
	}}
	watcher := NewMangaWatcher(d, provider, &SeriesDetector{db: d}, func() time.Time { return now })
	stats, err := watcher.SyncAndQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Queued != 0 {
		t.Fatalf("queued = %d, want 0 for disabled watcher", stats.Queued)
	}
}

func dbNewTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
