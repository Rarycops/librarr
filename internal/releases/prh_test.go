package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

func TestPRHProviderSearchSeries(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "prh_catalog_rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Fatal("expected search query")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	provider := NewPRHProvider(srv.Client(), "test-key", func() time.Time { return now }).WithBaseURL(srv.URL)

	releases, err := provider.SearchSeries(context.Background(), "Witch Hat Atelier", "Kamome Shirahama")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(releases))
	}
	got := releases[0]
	if got.ProviderKey != "9798888779781" || got.Sequence != 15 || got.Kind != models.ReleaseKindVolume {
		t.Fatalf("witch hat release = %#v", got)
	}
	if !got.OnSaleAt.Equal(time.Date(2026, 12, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("on sale = %v", got.OnSaleAt)
	}

	vinland, err := provider.SearchSeries(context.Background(), "Vinland Saga", "Makoto Yukimura")
	if err != nil {
		t.Fatal(err)
	}
	if len(vinland) != 1 {
		t.Fatalf("vinland releases = %d, want 1", len(vinland))
	}
	if !vinland[0].Final {
		t.Fatal("expected Vinland Saga final flag")
	}
	if !vinland[0].OnSaleAt.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("vinland on sale = %v", vinland[0].OnSaleAt)
	}
}

func TestPRHProviderSkipsMalformedRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"isbn":"bad","name":"No Date Product","seriesName":"Series","seriesNumber":1}]}`))
	}))
	defer srv.Close()

	provider := NewPRHProvider(srv.Client(), "test-key", time.Now).WithBaseURL(srv.URL)
	_, err := provider.SearchSeries(context.Background(), "Series", "")
	if err == nil {
		t.Fatal("expected error when all rows are malformed")
	}
}
