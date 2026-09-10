package models

import "time"

// ReleaseKind describes the unit a catalog or library release represents.
type ReleaseKind string

const (
	ReleaseKindUnknown ReleaseKind = "unknown"
	ReleaseKindVolume  ReleaseKind = "volume"
	ReleaseKindOmnibus ReleaseKind = "omnibus"
	ReleaseKindChapter ReleaseKind = "chapter"
)

// ReleaseMode controls which kinds an individual series watcher accepts.
type ReleaseMode string

const (
	ReleaseModeAuto    ReleaseMode = "auto"
	ReleaseModeVolume  ReleaseMode = "volume"
	ReleaseModeOmnibus ReleaseMode = "omnibus"
	ReleaseModeChapter ReleaseMode = "chapter"
	ReleaseModeAny     ReleaseMode = "any"
)

// MangaRelease is a dated, externally announced release candidate.
type MangaRelease struct {
	ID          int64       `json:"id,omitempty"`
	Provider    string      `json:"provider"`
	ProviderKey string      `json:"provider_key"`
	SeriesName  string      `json:"series_name"`
	Author      string      `json:"author,omitempty"`
	Title       string      `json:"title"`
	Kind        ReleaseKind `json:"kind"`
	Sequence    float64     `json:"sequence,omitempty"`
	OnSaleAt    time.Time   `json:"on_sale_at"`
	Final       bool        `json:"final"`
	SourceURL   string      `json:"source_url,omitempty"`
	FetchedAt   time.Time   `json:"fetched_at"`
}
