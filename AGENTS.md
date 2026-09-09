# Librarr Fork Agent Guide

This repository is the maintained fork of
[JeremiahM37/librarr](https://github.com/JeremiahM37/librarr).

## Source of truth

The source files, tests, Dockerfile, and workflows in this repository are
authoritative for `Rarycops/librarr`.

- `upstream` points to `JeremiahM37/librarr`.
- `origin` points to `Rarycops/librarr`.
- `main` is the deployable fork branch.
- HomeServer Helm wiring stays in
  `D:/Projects/HomeServer/apps/librarr`; do not copy Kubernetes secrets or
  cluster-specific scripts into this repository.
- Runtime database edits and container filesystem patches are diagnostics or
  migrations, never the durable implementation.

## Upstream synchronization

The nightly workflow at
[`.github/workflows/sync-upstream.yml`](.github/workflows/sync-upstream.yml)
fetches `upstream/main` and opens a review PR. Follow that same flow manually
when needed:

```bash
git fetch upstream main
git checkout main
git merge --no-commit --no-ff upstream/main
```

Resolve conflicts deliberately, run the checks below, then merge the PR.
Upstream changes never replace fork changes silently.

## Source changes

1. Reproduce the problem against the current source.
2. Add or update a focused Go test when behavior changes.
3. Keep integration behavior configurable; never hard-code private cluster
   URLs, credentials, or media paths.
4. Run `go test ./... -count=1` in Linux or GitHub Actions. POSIX-path tests
   are authoritative on Linux; Windows path separators can produce false
   failures.
5. Commit the source and tests together.

## Release and container promotion

The inherited [release workflow](.github/workflows/release.yml) is the
container pipeline. A tag is the promotion event:

```bash
version=1.3.8
test "$(tr -d '[:space:]' < internal/version/VERSION)" = "$version"
go test ./... -count=1
git tag -a "v$version" -m "Librarr v$version"
git push origin "v$version"
```

The workflow:

- validates `internal/version/VERSION` against the tag;
- runs the full Go test suite;
- publishes release artifacts;
- publishes
  `ghcr.io/rarycops/librarr:v<version>` and `ghcr.io/rarycops/librarr:latest`.

Do not publish an image manually from a workstation. Do not point HomeServer
at a tag until the GitHub release run and GHCR package are successful.

## HomeServer deployment handoff

After a successful tag build, update
`D:/Projects/HomeServer/apps/librarr/values.yaml` to the pinned fork tag:

```yaml
image:
  repository: ghcr.io/rarycops/librarr
  tag: "v1.3.8"
```

Preserve the deployment contract already used by HomeServer:

- Prowlarr URL and API key come from Kubernetes Secrets.
- qBittorrent URL and credentials come from Kubernetes Secrets.
- `/books/manga` maps to Jellyfin's `/media/manga`.
- `/downloads` and the persisted `/data/manga-incoming` path expose the same
  qBittorrent completion files.
- `LIBRARR_INSECURE_ALLOW_PRIVATE_URLS=1` is intentional for the internal
  Prowlarr service URL.
- `IMPORT_MODE=copy` preserves seeding while importing into Jellyfin.

Validate the HomeServer chart before pushing it:

```bash
helm lint apps/librarr
helm template librarr apps/librarr --namespace media
kubectl kustomize apps
git diff --check
```

Then verify Argo reports `Synced/Healthy`, Librarr health is `ok`, and both
Prowlarr and qBittorrent connection tests pass.

## Secrets and generated state

Credentials belong in GitHub Actions secrets or Kubernetes Secrets. Never
commit them, print them, or place them in workflow arguments. The Librarr
SQLite database, downloaded media, and import state are runtime data, not
source files.

## Completion criteria

A Librarr change is complete only when:

- the source and focused tests pass in Linux;
- the tagged GHCR image was built by GitHub Actions;
- HomeServer points at that immutable tag;
- Helm and Kustomize validation pass;
- Argo is `Synced/Healthy`;
- Prowlarr and qBittorrent tests pass;
- a representative manga acquisition/import path is verified.
