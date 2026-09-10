package releases

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/JeremiahM37/librarr/internal/models"
)

var (
	mangaOmnibusRe = regexp.MustCompile(`(?i)(?:^|[\s._-])omnibus(?:[\s._-]*(?:vol(?:ume)?\.?)?)?[\s._-]*0*(\d+(?:\.\d+)?)\b`)
	mangaChapterRe = regexp.MustCompile(`(?i)(?:^|[\s._-])chapter[\s._-]*0*(\d+(?:\.\d+)?)\b`)
	mangaVolumeRe  = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:volume|vol\.?|v)[\s._-]*0*(\d+(?:\.\d+)?)\b`)
)

// ParseMangaReleaseText extracts a numbered manga unit from a release title.
func ParseMangaReleaseText(title string) (models.ReleaseKind, float64, bool) {
	for _, candidate := range []struct {
		re   *regexp.Regexp
		kind models.ReleaseKind
	}{
		{mangaOmnibusRe, models.ReleaseKindOmnibus},
		{mangaChapterRe, models.ReleaseKindChapter},
		{mangaVolumeRe, models.ReleaseKindVolume},
	} {
		match := candidate.re.FindStringSubmatch(title)
		if len(match) != 2 {
			continue
		}
		sequence, err := strconv.ParseFloat(match[1], 64)
		if err == nil && sequence > 0 {
			return candidate.kind, sequence, true
		}
	}
	return models.ReleaseKindUnknown, 0, false
}

// DetectDominantReleaseKind chooses the most common parseable kind in a
// series' owned manga files.
func DetectDominantReleaseKind(items []models.LibraryItem) models.ReleaseKind {
	counts := map[models.ReleaseKind]int{}
	for _, item := range items {
		if item.MediaType != "manga" {
			continue
		}
		kind, _, ok := ParseMangaReleaseText(filepath.Base(item.FilePath))
		if ok {
			counts[kind]++
		}
	}
	best := models.ReleaseKindUnknown
	bestCount := 0
	for _, kind := range []models.ReleaseKind{
		models.ReleaseKindVolume,
		models.ReleaseKindOmnibus,
		models.ReleaseKindChapter,
	} {
		if counts[kind] > bestCount {
			best, bestCount = kind, counts[kind]
		}
	}
	return best
}

// ReleaseModeAccepts reports whether a catalog release matches watcher mode.
func ReleaseModeAccepts(mode models.ReleaseMode, detected, kind models.ReleaseKind) bool {
	switch mode {
	case models.ReleaseModeAny:
		return kind != models.ReleaseKindUnknown
	case models.ReleaseModeVolume, models.ReleaseModeOmnibus, models.ReleaseModeChapter:
		return kind == models.ReleaseKind(mode)
	default:
		return detected != models.ReleaseKindUnknown && kind == detected
	}
}

// NormalizeSeriesName lowercases a series name for comparisons.
func NormalizeSeriesName(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
