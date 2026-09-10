package db

import (
	"testing"
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

func TestSeriesWatchRoundTrip(t *testing.T) {
	d := newTestDB(t)

	if err := d.SetSeriesWatch("Witch Hat Atelier", true, models.ReleaseModeVolume); err != nil {
		t.Fatal(err)
	}
	enabled, mode, err := d.GetSeriesWatch("Witch Hat Atelier")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || mode != models.ReleaseModeVolume {
		t.Fatalf("watch state = %t, %q", enabled, mode)
	}

	tracking, err := d.GetSeriesTracking()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracking) != 1 || tracking[0]["watch_enabled"] != true || tracking[0]["release_mode"] != "volume" {
		t.Fatalf("tracking = %#v", tracking)
	}
}

func TestMangaReleaseUpsertAndList(t *testing.T) {
	d := newTestDB(t)
	release := models.MangaRelease{
		Provider:    "prh",
		ProviderKey: "9798888779781",
		SeriesName:  "Witch Hat Atelier",
		Author:      "Kamome Shirahama",
		Title:       "Witch Hat Atelier 15",
		Kind:        models.ReleaseKindVolume,
		Sequence:    15,
		OnSaleAt:    time.Date(2026, 12, 8, 0, 0, 0, 0, time.UTC),
		SourceURL:   "https://example.test/witch-hat-15",
		FetchedAt:   time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := d.UpsertMangaRelease(release); err != nil {
		t.Fatal(err)
	}
	release.Title = "Witch Hat Atelier 15 (updated)"
	if err := d.UpsertMangaRelease(release); err != nil {
		t.Fatal(err)
	}
	got, err := d.ListMangaReleases("Witch Hat Atelier", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != release.Title || got[0].Sequence != 15 {
		t.Fatalf("releases = %#v", got)
	}
}

func TestWishlistReleaseKeyRoundTrip(t *testing.T) {
	d := newTestDB(t)
	id, err := d.AddWishlistItemWithOptions(models.WishlistItem{
		Title:      "Witch Hat Atelier 15",
		MediaType:  "manga",
		Monitored:  true,
		ReleaseKey: "prh:9798888779781",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := d.GetWishlistItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.ReleaseKey != "prh:9798888779781" {
		t.Fatalf("release key = %q", item.ReleaseKey)
	}
	found, err := d.FindWishlistByReleaseKey("prh:9798888779781")
	if err != nil || found == nil || found.ID != id {
		t.Fatalf("release lookup = %#v, %v", found, err)
	}
}
