package web

import (
	"strings"
	"testing"
)

// These routes are the data contracts used by the React Wanted and Settings
// components. E2E exercises their interactions; this catches a stale bundle
// being embedded without the feature entrypoints being compiled at all.
func TestWantedBundleContainsFeatureRoutes(t *testing.T) {
	b, err := StaticFS.ReadFile("static/react/librarr.js")
	if err != nil {
		t.Fatal(err)
	}
	bundle := string(b)
	for _, route := range []string{"/api/wishlist", "/api/scheduler/run"} {
		if !strings.Contains(bundle, route) {
			t.Errorf("React bundle does not include Wanted feature route %q", route)
		}
	}
}

func TestWantedSearchCarriesAuthor(t *testing.T) {
	js := appJS(t)
	for _, want := range []string{
		`data-author="${escapeHtml(item.author || '')}"`,
		`searchWishlistItem(el.dataset.title, el.dataset.mediaType, el.dataset.author)`,
		"doSearch(title, author)",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("wanted search is missing author propagation: %s", want)
		}
	}
}

func TestMangaWatcherUIContract(t *testing.T) {
	js := appJS(t)
	for _, want := range []string{
		`data-action="toggleSeriesWatch"`,
		`aria-pressed="${watchEnabled ? 'true' : 'false'}"`,
		"toggleSeriesWatch:",
		"mangaWatchSummary(",
		"formatMangaNextRelease(",
		"manga_catalog_stale",
		"manga_final_release",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("manga watcher UI is missing %q", want)
		}
	}
}
