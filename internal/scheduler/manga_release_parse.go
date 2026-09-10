package scheduler

import (
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/releases"
)

// ParseMangaReleaseText extracts a numbered manga unit from a release title.
func ParseMangaReleaseText(title string) (models.ReleaseKind, float64, bool) {
	return releases.ParseMangaReleaseText(title)
}

// DetectDominantReleaseKind chooses the most common parseable kind in a series.
func DetectDominantReleaseKind(items []models.LibraryItem) models.ReleaseKind {
	return releases.DetectDominantReleaseKind(items)
}

func releaseModeAccepts(mode models.ReleaseMode, detected, kind models.ReleaseKind) bool {
	return releases.ReleaseModeAccepts(mode, detected, kind)
}

func normalizeSeriesName(value string) string {
	return releases.NormalizeSeriesName(value)
}
