package db

import (
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

// UpsertMangaRelease stores the latest catalog facts for one provider record.
func (d *DB) UpsertMangaRelease(release models.MangaRelease) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`INSERT INTO manga_releases
			(provider, provider_key, series_name, author, title, kind, sequence,
			 on_sale_at, final_release, source_url, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider, provider_key) DO UPDATE SET
			series_name = excluded.series_name,
			author = excluded.author,
			title = excluded.title,
			kind = excluded.kind,
			sequence = excluded.sequence,
			on_sale_at = excluded.on_sale_at,
			final_release = excluded.final_release,
			source_url = excluded.source_url,
			fetched_at = excluded.fetched_at`,
		release.Provider, release.ProviderKey, release.SeriesName, release.Author,
		release.Title, release.Kind, release.Sequence, release.OnSaleAt.Unix(),
		boolToInt(release.Final), release.SourceURL, release.FetchedAt.Unix(),
	)
	return err
}

// ListMangaReleases returns catalog records for a series from a date onward.
func (d *DB) ListMangaReleases(seriesName string, from time.Time) ([]models.MangaRelease, error) {
	rows, err := d.db.Query(
		`SELECT id, provider, provider_key, series_name, author, title, kind,
			sequence, on_sale_at, final_release, source_url, fetched_at
		 FROM manga_releases
		 WHERE series_name = ? AND on_sale_at >= ?
		 ORDER BY on_sale_at, sequence, title`,
		seriesName, from.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []models.MangaRelease
	for rows.Next() {
		var release models.MangaRelease
		var onSaleAt, fetchedAt float64
		var finalRelease int
		if err := rows.Scan(
			&release.ID, &release.Provider, &release.ProviderKey,
			&release.SeriesName, &release.Author, &release.Title, &release.Kind,
			&release.Sequence, &onSaleAt, &finalRelease, &release.SourceURL,
			&fetchedAt,
		); err != nil {
			return nil, err
		}
		release.OnSaleAt = time.Unix(int64(onSaleAt), 0).UTC()
		release.Final = finalRelease != 0
		release.FetchedAt = time.Unix(int64(fetchedAt), 0).UTC()
		releases = append(releases, release)
	}
	return releases, rows.Err()
}
