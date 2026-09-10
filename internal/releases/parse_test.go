package releases

import (
	"testing"

	"github.com/JeremiahM37/librarr/internal/models"
)

func TestParseMangaReleaseText(t *testing.T) {
	tests := []struct {
		title string
		kind  models.ReleaseKind
		seq   float64
		ok    bool
	}{
		{"Delicious in Dungeon v14 (2024) (Digital)", models.ReleaseKindVolume, 14, true},
		{"Vinland Saga Omnibus 15", models.ReleaseKindOmnibus, 15, true},
		{"Witch Hat Atelier Chapter 101", models.ReleaseKindChapter, 101, true},
		{"Manga Complete Edition", models.ReleaseKindUnknown, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			kind, seq, ok := ParseMangaReleaseText(tt.title)
			if kind != tt.kind || seq != tt.seq || ok != tt.ok {
				t.Fatalf("ParseMangaReleaseText(%q) = %q, %v, %t; want %q, %v, %t",
					tt.title, kind, seq, ok, tt.kind, tt.seq, tt.ok)
			}
		})
	}
}
