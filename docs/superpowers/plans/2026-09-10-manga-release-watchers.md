# Manga Release Watchers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-series manga watchers backed by official Penguin Random House/Kodansha catalog dates, while reusing Librarr’s existing wanted scheduler and manga import pipeline.

**Architecture:** Keep `series_tracking` as the watcher/control-plane record and add a normalized `manga_releases` catalog table. A PRH catalog provider fetches public monthly title-list data, the watcher queues only due future releases into the existing wishlist, and the current scheduler handles search, quality, qBittorrent, import, and wanted linking. The manga UI receives canonical series identity plus watcher/calendar state from `/api/series`.

**Tech Stack:** Go 1.25, SQLite via `modernc.org/sqlite`, `goquery` for catalog HTML, `github.com/extrame/xls v0.0.1` for public PRH legacy XLS catalogs, embedded JavaScript UI, existing Go test suite.

## Global Constraints

- New watchers only acquire releases newer than the highest owned unit; they do not backfill gaps.
- Watchers are per canonical manga series card, including one-item series.
- Release modes are exactly `auto`, `volume`, `omnibus`, `chapter`, and `any`.
- Ambiguous or unnumbered releases are never auto-downloaded.
- An active watcher with no official catalog match waits without using torrent-title heuristics.
- Final releases remain watched after acquisition and display “final acquired.”
- Catalog sync is read-only, deterministic in tests, and never stores credentials.
- Existing wanted rows and download/import/linking remain the only acquisition pipeline.
- Do not add a second scheduler or a second torrent/import implementation.

---

### Task 1: Add release and watcher domain types

**Files:**
- Create: `internal/models/manga_release.go`
- Modify: `internal/models/book.go`
- Test: `internal/models/manga_release_test.go`

**Interfaces:**
- `models.ReleaseKind` values: `unknown`, `volume`, `omnibus`, `chapter`.
- `models.ReleaseMode` values: `auto`, `volume`, `omnibus`, `chapter`, `any`.
- `models.MangaRelease` fields:
  `Provider`, `ProviderKey`, `SeriesName`, `Author`, `Title`,
  `Kind`, `Sequence`, `OnSaleAt`, `Final`, `SourceURL`,
  `FetchedAt`.
- Extend `models.WishlistItem` with `ReleaseKey string`.
- Extend `models.LibraryItem` JSON-facing data with `SeriesName string` when the
  API can derive it; do not persist a duplicate series column in this task.

- [ ] **Step 1: Write failing pure-model tests**

Test that:

```go
func TestReleaseKindValuesAreStable(t *testing.T) {
    if models.ReleaseKindOmnibus != "omnibus" {
        t.Fatal("release kind is not API-stable")
    }
}
```

and that an empty `ReleaseKey` remains valid for existing manual wishlist rows.

- [ ] **Step 2: Run the focused test**

Run:

```powershell
go test ./internal/models -run 'TestReleaseKindValuesAreStable' -count=1
```

Expected: FAIL because the new types do not exist.

- [ ] **Step 3: Implement the types and JSON fields**

Use string-backed constants so SQLite/API values remain readable and forward
compatible. Keep `Sequence` as `float64` for values such as `1.5` and
`OnSaleAt` as a UTC `time.Time` serialized by the API.

- [ ] **Step 4: Run the focused test**

Run the same command and expect PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/models/manga_release.go internal/models/book.go internal/models/manga_release_test.go
git commit -m "Add manga release domain types"
```

---

### Task 2: Persist watcher controls, releases, and release-backed wishlist rows

**Files:**
- Modify: `internal/db/db.go:222-360`
- Modify: `internal/db/series.go`
- Modify: `internal/db/wishlist.go`
- Modify: `internal/db/library.go`
- Create: `internal/db/manga_releases.go`
- Test: `internal/db/series_test.go`
- Test: `internal/db/wanted_test.go`
- Create: `internal/db/manga_releases_test.go`

**Interfaces:**
- Extend `series_tracking` with:
  `watch_enabled INTEGER NOT NULL DEFAULT 0`,
  `release_mode TEXT NOT NULL DEFAULT 'auto'`,
  `detected_release_kind TEXT NOT NULL DEFAULT ''`,
  `highest_owned_unit REAL NOT NULL DEFAULT 0`,
  `catalog_last_sync REAL NOT NULL DEFAULT 0`,
  `catalog_error TEXT NOT NULL DEFAULT ''`.
- Add `manga_releases`:

```sql
CREATE TABLE IF NOT EXISTS manga_releases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider TEXT NOT NULL,
  provider_key TEXT NOT NULL,
  series_name TEXT NOT NULL,
  author TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'unknown',
  sequence REAL NOT NULL DEFAULT 0,
  on_sale_at REAL NOT NULL,
  final_release INTEGER NOT NULL DEFAULT 0,
  source_url TEXT NOT NULL DEFAULT '',
  fetched_at REAL NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_manga_releases_provider_key
  ON manga_releases(provider, provider_key);
CREATE INDEX IF NOT EXISTS idx_manga_releases_series_date
  ON manga_releases(series_name, on_sale_at);
```

- Add `release_key TEXT NOT NULL DEFAULT ''` to `wishlist` and index nonempty
  values.
- `DB.SetSeriesWatch(name string, enabled bool, mode models.ReleaseMode) error`
  creates the tracking row if absent and updates only watcher fields.
- `DB.GetSeriesWatch(name string) (enabled bool, mode models.ReleaseMode, error)`
- `DB.GetSeriesTracking()` returns watcher fields in addition to current counts.
- `DB.UpsertMangaRelease(models.MangaRelease) error`
- `DB.ListMangaReleases(seriesName string, from time.Time) ([]models.MangaRelease,error)`
- `DB.FindWishlistByReleaseKey(key string) (*models.WishlistItem,error)`
- `DB.AddWishlistItemWithOptions` persists `ReleaseKey`.

- [ ] **Step 1: Add failing migration/round-trip tests**

Cover:

```go
enabled, mode, err := database.GetSeriesWatch("Vinland Saga")
if err != nil || enabled || mode != models.ReleaseModeAuto {
    t.Fatalf("default watcher state = %v, %q, %v", enabled, mode, err)
}
```

Also verify:

- an old database migrates with watcher defaults;
- toggling a missing series creates its row;
- a release upsert is idempotent by provider/key;
- two wishlist rows cannot be created for the same release key.

- [ ] **Step 2: Run the focused DB tests**

```powershell
go test ./internal/db -run 'TestSeries|TestMangaRelease|TestWishlistReleaseKey' -count=1
```

Expected: FAIL before migration/API methods exist.

- [ ] **Step 3: Implement additive migrations and DB methods**

Preserve all existing watcher values on series upsert. Use SQLite `INSERT ...
ON CONFLICT` for release upserts and `release_key = ''` for legacy/manual rows.

- [ ] **Step 4: Run the focused DB tests**

Expect PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/db/db.go internal/db/series.go internal/db/wishlist.go internal/db/library.go internal/db/manga_releases.go internal/db/series_test.go internal/db/wanted_test.go internal/db/manga_releases_test.go
git commit -m "Persist manga watcher and release state"
```

---

### Task 3: Implement pure release-kind and catalog-row parsing

**Files:**
- Create: `internal/scheduler/manga_release_parse.go`
- Create: `internal/scheduler/manga_release_parse_test.go`
- Create: `internal/releases/prh.go`
- Create: `internal/releases/prh_test.go`
- Create: `internal/releases/testdata/prh_catalog_rows.json`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- `ParseMangaReleaseText(title string) (kind models.ReleaseKind, sequence float64, ok bool)`
- `DetectDominantReleaseKind(items []models.LibraryItem) models.ReleaseKind`
- `type CatalogProvider interface { Name() string; Sync(context.Context) ([]models.MangaRelease, error) }`
- `func NewPRHProvider(client *http.Client, now func() time.Time) *PRHProvider`

Parsing rules:

- `Volume 14`, `Vol. 14`, and `v14` → `volume`, sequence `14`.
- `Omnibus 3` or `Omnibus Vol. 3` → `omnibus`, sequence `3`.
- `Chapter 101` or `Ch. 101` → `chapter`, sequence `101`.
- Parenthesized years, release groups, `[Digital]`, and file extensions do
  not contribute to sequence detection.
- A release without a reliable sequence returns `unknown, 0, false`.
- PRH rows use their explicit series number first, then title parsing.
- `FINAL VOLUME`/`FINAL` in the catalog description sets `Final=true`.
- A PRH release key is its ISBN/UPC string.

The PRH provider discovers recent catalog landing pages from the public archive,
extracts catalog download links, parses the XLS rows, and returns only rows
containing a usable on-sale date and title/series identity. It keeps source
URLs on every row. Tests use fixture data and an `httptest.Server`; no public
network calls run in tests.

- [ ] **Step 1: Write parser tests**

Include literal fixtures for:

```go
{"Delicious in Dungeon v14 (2024) (Digital)", volume, 14}
{"Vinland Saga 15", omnibus, 15}
{"Chapter 101", chapter, 101}
{"Untitled Complete", unknown, 0}
```

Include catalog fixtures for Witch Hat Atelier 15 and Vinland Saga 15 with
their official dates and final flag.

- [ ] **Step 2: Run parser tests and observe failure**

```powershell
go test ./internal/scheduler ./internal/releases -run 'TestParseManga|TestPRH' -count=1
```

- [ ] **Step 3: Add the minimal parser/provider implementation**

Use the existing `goquery` dependency for catalog HTML discovery. Add only the
smallest XLS reader needed for PRH’s public catalog format; do not introduce a
general spreadsheet abstraction.

- [ ] **Step 4: Run parser tests**

Expect PASS and verify malformed catalog rows are skipped with an error count,
not fatal process failure.

- [ ] **Step 5: Commit**

```powershell
git add go.mod go.sum internal/scheduler/manga_release_parse.go internal/scheduler/manga_release_parse_test.go internal/releases/prh.go internal/releases/prh_test.go internal/releases/testdata/prh_catalog_rows.json
git commit -m "Parse official manga release catalog records"
```

---

### Task 4: Extend series detection and expose watcher state

**Files:**
- Modify: `internal/scheduler/series.go`
- Modify: `internal/db/series.go`
- Modify: `internal/api/scheduler_handler.go`
- Modify: `internal/api/router.go`
- Create: `internal/api/series_watcher_test.go`
- Modify: `internal/scheduler/series_test.go`

**Interfaces:**
- Extend `SeriesInfo` with:
  `WatchEnabled bool`,
  `ReleaseMode string`,
  `DetectedReleaseKind string`,
  `HighestOwnedUnit float64`,
  `NextRelease *models.MangaRelease`,
  `CatalogStale bool`,
  `CatalogError string`.
- Add:

```go
PATCH /api/series/{name}/watch
{
  "enabled": true,
  "release_mode": "auto"
}
```

The route requires admin access and returns the updated watcher state.
`GET /api/series` must include every canonical manga directory, even when it
contains one item or no parseable unit. It merges DB watcher state with
detected owned units and the next catalog release.

- [ ] **Step 1: Add failing API tests**

Cover:

- GET returns a one-volume series;
- PATCH enables/disables a watcher;
- invalid release mode returns 400;
- enabling a previously unseen series creates its tracking row;
- an official future release appears in `next_release`.

- [ ] **Step 2: Run API tests and verify failure**

```powershell
go test ./internal/api -run 'TestSeriesWatcher' -count=1
```

- [ ] **Step 3: Implement detection merge and PATCH handler**

Derive canonical series identity from the first-level directory under
`MangaDir`, not from raw torrent titles. Preserve existing missing-volume
calculation for the modal, but do not use it to auto-create downloads.

- [ ] **Step 4: Run API and series tests**

```powershell
go test ./internal/api ./internal/scheduler -run 'TestSeries|TestMangaVolume|TestSeriesWatcher' -count=1
```

- [ ] **Step 5: Commit**

```powershell
git add internal/scheduler/series.go internal/scheduler/series_test.go internal/db/series.go internal/api/scheduler_handler.go internal/api/router.go internal/api/series_watcher_test.go
git commit -m "Expose per-series manga watcher controls"
```

---

### Task 5: Queue due catalog releases through the existing wanted scheduler

**Files:**
- Create: `internal/scheduler/manga_watcher.go`
- Create: `internal/scheduler/manga_watcher_test.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/librarr/main.go`
- Modify: `internal/db/wishlist.go`

**Interfaces:**
- `NewMangaWatcher(database *db.DB, provider releases.CatalogProvider, now func() time.Time) *MangaWatcher`
- `MangaWatcher.SyncAndQueue(context.Context) (MangaWatchStats, error)`
- `Scheduler.SetMangaWatcher(*MangaWatcher)`

`SyncAndQueue` must:

1. Sync PRH releases and preserve the last good catalog on provider errors.
2. Refresh canonical series owned units and watcher state.
3. Ignore disabled watchers.
4. Ignore future releases until `on_sale_at <= now`.
5. Ignore releases not newer than `highest_owned_unit`.
6. Apply `release_mode`/detected kind.
7. Ignore unknown/ambiguous releases.
8. Use `release_key` to avoid duplicate wanted rows.
9. Create a monitored manga wishlist row with source
   `series-watcher:<provider>` and the catalog title/author.
10. Leave final watchers enabled after acquisition.

Call `SyncAndQueue` once at the beginning of `Scheduler.RunCtx`, before
processing the wishlist. A provider failure should be recorded and should not
prevent existing wanted rows from being processed.

- [ ] **Step 1: Add failing watcher tests**

Use a fake catalog provider and frozen clock to test:

- future Witch Hat release is displayed but not queued;
- due Vinland final release is queued;
- old gaps are not queued;
- disabled watcher is ignored;
- unknown sequence is ignored;
- repeated runs create one wishlist row;
- existing release-key row is not duplicated.

- [ ] **Step 2: Run watcher tests and verify failure**

```powershell
go test ./internal/scheduler -run 'TestMangaWatcher' -count=1
```

- [ ] **Step 3: Implement `MangaWatcher` and scheduler wiring**

Keep provider/network errors in watcher state; return operational errors only
for database failures that prevent safe scheduling.

- [ ] **Step 4: Run the scheduler/API focused suite**

```powershell
go test ./internal/api ./internal/db ./internal/scheduler -count=1
```

- [ ] **Step 5: Commit**

```powershell
git add internal/scheduler/manga_watcher.go internal/scheduler/manga_watcher_test.go internal/scheduler/scheduler.go internal/api/router.go cmd/librarr/main.go internal/db/wishlist.go
git commit -m "Queue due manga watcher releases"
```

---

### Task 6: Add watcher controls and calendar details to the manga UI

**Files:**
- Modify: `web/static/js/app.js`
- Modify: `web/wanted_ui_test.go`
- Modify: `web/index.html` only if a shared accessible control style/container is required

**Interfaces:**
- Add `toggleSeriesWatch(seriesName, enabled, releaseMode)`.
- Register `toggleSeriesWatch` in `CLICK_ACTIONS`.
- Render an accessible button on every manga card:

```html
<button aria-pressed="false" data-action="toggleSeriesWatch">
  Watch
</button>
```

The card must show:

- Watching / Not watching;
- `Volume`, `Omnibus`, `Chapter`, or `Auto`;
- next official release date/title;
- Final release;
- Catalog stale/error.

On toggle success, update `state.seriesTracking` and rerender the card without
reloading the entire library. On failure, restore the previous state and show a
toast. The current “Request missing” modal action remains separate.

- [ ] **Step 1: Add UI contract tests**

Assert that:

- every manga card emits a watcher action;
- `aria-pressed` is present;
- the action is registered;
- calendar/final/stale fields have render paths.

- [ ] **Step 2: Run the UI test and verify failure**

```powershell
go test ./web -run 'TestSeries|TestMangaWatcher' -count=1
```

- [ ] **Step 3: Implement rendering and toggle behavior**

Keep all copy in the English/Russian translation tables and use existing event
delegation rather than inline handlers.

- [ ] **Step 4: Run UI and API contract tests**

```powershell
go test ./web ./internal/api -count=1
```

- [ ] **Step 5: Commit**

```powershell
git add web/static/js/app.js web/index.html web/wanted_ui_test.go
git commit -m "Add manga watcher controls and release calendar UI"
```

---

### Task 7: Full verification, release, and GitOps deployment

**Files:**
- Modify: `internal/version/VERSION`
- Modify: `D:\Projects\HomeServer\apps\librarr\values.yaml`

- [ ] **Step 1: Run local verification**

```powershell
go test ./... -count=1
go test ./... -count=1 -race
git diff --check
```

Expected: all Linux-authoritative tests pass in GitHub Actions. Windows-only
path tests may require WSL/Linux because they intentionally assert POSIX paths.

- [ ] **Step 2: Bump the version and publish**

Set `internal/version/VERSION` to the next patch version, commit, push `main`,
create the matching `vX.Y.Z` tag, and push the tag. Wait for release tests,
Docker, CodeQL, and the GitHub release to succeed.

- [ ] **Step 3: Pin HomeServer**

Update `apps/librarr/values.yaml` to the successful immutable tag, then run:

```powershell
helm lint apps/librarr
helm template librarr apps/librarr --namespace media
kubectl kustomize apps
git diff --check
git add apps/librarr/values.yaml
git commit -m "Deploy Librarr v1.3.16"
git push origin main
```

- [ ] **Step 4: Verify production**

Confirm Argo reports `librarr` `Synced/Healthy`, the pod image is the new tag,
`GET /api/series` reports watcher/calendar state, and a deterministic catalog
fixture test plus one live read-only series listing succeeds.
