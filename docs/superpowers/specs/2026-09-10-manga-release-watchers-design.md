# Manga Release Watchers Design

**Date:** 2026-09-10

## Goal

Add reliable, date-aware manga watchers to Librarr. Every manga series card can
be toggled active/inactive. Active watchers wait for official catalog dates and
then feed future volume, omnibus, or chapter releases into the existing wanted
and download pipeline.

## Decisions

- Watcher scope is **new releases only**. Existing gaps remain manual through
  the current missing-volume request flow.
- Watchers are per **series card**, including one-shot and single-volume manga.
- Release kind is auto-detected from owned files/catalog records, with a
  per-series override: `auto`, `volume`, `omnibus`, `chapter`, or `any`.
- Ambiguous or unnumbered releases are never auto-downloaded; they are shown as
  needing review.
- Watchers reuse the existing scheduler interval, manga quality profile,
  wanted rows, qBittorrent routing, and import/linking behavior.
- Before an official catalog match exists, an active watcher waits and does not
  use torrent-title heuristics.
- A catalog-marked final release does not automatically deactivate its watcher;
  the UI reports that the final release was acquired.

## Release calendar source

The first provider is the public Penguin Random House Comics Retail catalog,
which covers the Kodansha English releases needed by the initial use cases.
Monthly catalog pages expose title lists and product records containing ISBN,
series number, on-sale date, publisher, and release descriptions.

The provider will:

1. Discover the latest public catalog pages.
2. Fetch the downloadable catalog data.
3. Parse and normalize product rows.
4. Upsert release records keyed by provider + ISBN.
5. Preserve the last successful sync and expose sync age/errors.

The provider is read-only and does not require retailer credentials. A catalog
date is authoritative for automatic acquisition. The implementation must not
fall back to search-engine snippets or torrent titles for date decisions.

The verified examples are Witch Hat Atelier 15 on December 8, 2026 and
Vinland Saga 15 on October 6, 2026; the latter is the final English volume.

## Domain model

Extend the existing `series_tracking` record with:

- `watch_enabled` — default `false`;
- `release_mode` — default `auto`;
- `detected_release_kind`;
- `highest_owned_unit`;
- `last_catalog_sync`;
- `catalog_error`.

Add a `manga_releases` table:

- provider and provider key/ISBN;
- canonical series name and author;
- catalog title;
- release kind: `volume`, `omnibus`, `chapter`, or `unknown`;
- sequence number when parseable;
- on-sale date;
- final-release flag;
- official source URL;
- fetched/updated timestamps.

Series identity is the canonical first-level directory under the configured
manga root, not the raw torrent title. Library API rows used by the manga UI
must expose that canonical series name so release names cannot split one series
into separate cards.

## Data flow

1. The catalog sync runs as part of the existing scheduler cadence and updates
   `manga_releases`.
2. Series detection refreshes owned unit/kind information and ensures a
   tracking record exists for every manga series card.
3. The UI toggle updates `watch_enabled` and optionally `release_mode`.
4. For each enabled watcher, the scheduler selects the next catalog release
   whose:
   - series and author match with sufficient confidence;
   - kind matches the detected/overridden mode;
   - sequence is greater than the highest owned sequence;
   - on-sale date is today or earlier;
   - release is not already owned, wanted, or downloading.
5. The scheduler creates or updates one manga wanted row for that release.
6. The existing wanted scheduler searches sources and downloads using the manga
   profile.
7. Import refreshes owned unit state and links the wanted row as today.
8. Future-dated releases remain visible as upcoming calendar entries and do
   not create active download jobs.

`auto` chooses the dominant parseable kind from the owned library. `any`
accepts any parseable kind but still requires a sequence and a catalog date.
A final release remains visible after acquisition and does not turn the
watcher off.

## API and UI

Add:

- `PATCH /api/series/{name}/watch` with `{enabled, release_mode?}`;
- release/calendar fields to `GET /api/series`;
- watcher state and next release details to manga series cards.

Each manga card gets an accessible toggle with `aria-pressed`, showing
`Watching` or `Not watching`. The card also shows:

- detected release kind;
- next official release title/date;
- final-release badge when applicable;
- stale/error state when catalog sync is unavailable.

The toggle is independent of the wanted row. Turning it off stops future
watcher-created wanted rows but does not delete existing wanted rows or files.

## Safety and failure behavior

- Catalog parse failures retain the last successful records and mark the
  provider stale.
- A stale provider never schedules a new automatic download.
- Ambiguous series matches are surfaced as review candidates, not grabbed.
- Duplicate wanted rows are prevented by provider key/series sequence.
- Existing gaps are not silently filled.
- Release descriptions and source URLs are retained for auditability.

## Testing

Add focused tests for:

- PRH catalog row parsing, including Witch Hat Atelier and Vinland Saga
  fixtures;
- volume, omnibus, chapter, unknown, and final-release detection;
- date gating and new-only sequence selection;
- no scheduling when the catalog is stale or a match is ambiguous;
- watcher enable/disable and release-mode API persistence;
- one-item series detection;
- canonical series grouping and UI watcher action registration;
- wanted-row creation without duplicate rows;
- the existing manga download/import/linking seam.

No watcher test may call a real catalog or indexer. Catalog responses and
current dates must be deterministic fixtures.
