package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

// --- Series Tracking ---

// UpsertSeriesTracking creates or updates a series tracking entry.
func (d *DB) UpsertSeriesTracking(seriesName string, knownTotal, ownedCount int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(
		`INSERT INTO series_tracking (series_name, known_total, owned_count, last_checked)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(series_name) DO UPDATE SET known_total = ?, owned_count = ?, last_checked = ?`,
		seriesName, knownTotal, ownedCount, float64(time.Now().Unix()),
		knownTotal, ownedCount, float64(time.Now().Unix()),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// SetSeriesWatch enables or disables automatic watching for a series.
func (d *DB) SetSeriesWatch(seriesName string, enabled bool, mode models.ReleaseMode) error {
	if mode == "" {
		mode = models.ReleaseModeAuto
	}
	switch mode {
	case models.ReleaseModeAuto, models.ReleaseModeVolume, models.ReleaseModeOmnibus,
		models.ReleaseModeChapter, models.ReleaseModeAny:
	default:
		return fmt.Errorf("invalid release mode %q", mode)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(
		`INSERT INTO series_tracking (series_name, watch_enabled, release_mode, last_checked)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(series_name) DO UPDATE SET watch_enabled = ?, release_mode = ?`,
		seriesName, boolToInt(enabled), mode, float64(time.Now().Unix()),
		boolToInt(enabled), mode,
	)
	return err
}

// GetSeriesWatch returns watcher state, defaulting to disabled/auto for a
// series that has not been tracked yet.
func (d *DB) GetSeriesWatch(seriesName string) (bool, models.ReleaseMode, error) {
	var enabled int
	var mode string
	err := d.db.QueryRow(
		`SELECT watch_enabled, release_mode FROM series_tracking WHERE series_name = ?`,
		seriesName,
	).Scan(&enabled, &mode)
	if err == sql.ErrNoRows {
		return false, models.ReleaseModeAuto, nil
	}
	if err != nil {
		return false, models.ReleaseModeAuto, err
	}
	if mode == "" {
		mode = string(models.ReleaseModeAuto)
	}
	return enabled != 0, models.ReleaseMode(mode), nil
}

// GetSeriesTracking returns all tracked series.
func (d *DB) GetSeriesTracking() ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT id, series_name, known_total, owned_count,
		watch_enabled, release_mode, detected_release_kind, highest_owned_unit,
		catalog_last_sync, catalog_error, last_checked
		FROM series_tracking ORDER BY series_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id int64
		var name string
		var total, owned int
		var watchEnabled int
		var releaseMode, detectedKind, catalogError string
		var highestOwned, catalogLastSync, lastChecked float64
		if err := rows.Scan(&id, &name, &total, &owned, &watchEnabled, &releaseMode,
			&detectedKind, &highestOwned, &catalogLastSync, &catalogError, &lastChecked); err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"id":                    id,
			"series_name":           name,
			"known_total":           total,
			"owned_count":           owned,
			"watch_enabled":         watchEnabled != 0,
			"release_mode":          releaseMode,
			"detected_release_kind": detectedKind,
			"highest_owned_unit":    highestOwned,
			"catalog_last_sync":     time.Unix(int64(catalogLastSync), 0).Format(time.RFC3339),
			"catalog_error":         catalogError,
			"last_checked":          time.Unix(int64(lastChecked), 0).Format(time.RFC3339),
		})
	}
	return result, nil
}
