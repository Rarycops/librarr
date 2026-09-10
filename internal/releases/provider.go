package releases

import (
	"context"

	"github.com/JeremiahM37/librarr/internal/models"
)

// CatalogProvider fetches official release schedules for watched manga series.
type CatalogProvider interface {
	Name() string
	SearchSeries(ctx context.Context, seriesName, author string) ([]models.MangaRelease, error)
}
