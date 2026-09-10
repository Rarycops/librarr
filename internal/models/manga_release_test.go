package models

import "testing"

func TestReleaseKindValuesAreStable(t *testing.T) {
	if ReleaseKindOmnibus != "omnibus" || ReleaseKindChapter != "chapter" {
		t.Fatalf("release kind constants changed: %q %q", ReleaseKindOmnibus, ReleaseKindChapter)
	}
	if ReleaseModeAuto != "auto" || ReleaseModeAny != "any" {
		t.Fatalf("release mode constants changed: %q %q", ReleaseModeAuto, ReleaseModeAny)
	}
}

func TestWishlistReleaseKeyMayBeEmpty(t *testing.T) {
	item := WishlistItem{Title: "Manual Manga", MediaType: "manga"}
	if item.ReleaseKey != "" {
		t.Fatalf("manual wishlist item release key = %q, want empty", item.ReleaseKey)
	}
}
