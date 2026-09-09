package scheduler

import (
	"testing"

	"github.com/JeremiahM37/librarr/internal/models"
)

func TestMangaVolume(t *testing.T) {
	detector := &SeriesDetector{mangaRoot: "/books/manga"}
	tests := []struct {
		name   string
		path   string
		series string
		volume int
		ok     bool
	}{
		{
			name:   "nested volume folder",
			path:   "/books/manga/Pluto/Volume 08 (2004)/Pluto (2004) Volume 008.cbz",
			series: "Pluto",
			volume: 8,
			ok:     true,
		},
		{
			name:   "direct volume filename",
			path:   "/books/manga/Monster/Monster v18.cbr",
			series: "Monster",
			volume: 18,
			ok:     true,
		},
		{
			name: "outside root",
			path: "/downloads/Monster/monster v18.cbr",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			series, volume, ok := detector.mangaVolume(models.LibraryItem{
				MediaType: "manga",
				FilePath:  tc.path,
			})
			if ok != tc.ok || series != tc.series || volume != tc.volume {
				t.Fatalf("mangaVolume() = (%q, %d, %t), want (%q, %d, %t)",
					series, volume, ok, tc.series, tc.volume, tc.ok)
			}
		})
	}
}
