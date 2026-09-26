// Package organize moves downloaded ebooks, audiobooks, and manga into
// the configured library layout, extracting metadata where possible.
package organize

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JeremiahM37/librarr/internal/config"
	"github.com/JeremiahM37/librarr/internal/models"
	"github.com/JeremiahM37/librarr/internal/releases"
)

// Organizer handles post-download file organization.
type Organizer struct {
	cfg *config.Config

	// forceMove ignores the configured import mode. Set by Moving() for
	// sources that are not download-client payloads.
	forceMove bool
}

// NewOrganizer creates a new file organizer.
func NewOrganizer(cfg *config.Config) *Organizer {
	return &Organizer{cfg: cfg}
}

// Moving returns an organizer that always moves, whatever IMPORT_MODE says.
// Uploads and other librarr-owned temporary files have nothing to seed, so
// hardlinking or copying them would leave the original behind forever.
func (o *Organizer) Moving() *Organizer {
	clone := *o
	clone.forceMove = true
	return &clone
}

// importMode returns the effective import mode for this organizer.
func (o *Organizer) importMode() string {
	if o.forceMove || o.cfg == nil {
		return config.ImportModeMove
	}
	return o.cfg.EffectiveImportMode()
}

// KeepsPayload reports whether imports leave the source files where the
// download client put them, which is what seeding requires. Callers use it to
// decide what to do with the download after a successful import.
func (o *Organizer) KeepsPayload() bool {
	return o.importMode() != config.ImportModeMove
}

// placeFile puts src at dst using the configured import mode. Only the move
// mode removes src.
func (o *Organizer) placeFile(src, dst string) error {
	switch o.importMode() {
	case config.ImportModeHardlink:
		return hardlinkFile(src, dst)
	case config.ImportModeCopy:
		return copyFileForOrg(src, dst)
	default:
		return moveFile(src, dst)
	}
}

// OrganizeEbook moves an ebook file into the organized directory structure: {EbookDir}/{Author}/{Title}/{file}
// Also copies to KAVITA_LIBRARY_PATH if configured.
func (o *Organizer) OrganizeEbook(filePath, title, author string) (string, error) {
	if !o.cfg.FileOrgEnabled {
		return filePath, nil
	}

	if author == "" {
		// Try to extract author from EPUB metadata.
		if strings.HasSuffix(strings.ToLower(filePath), ".epub") {
			if meta, err := ExtractEPUBMeta(filePath); err == nil && meta.Author != "" {
				author = meta.Author
			}
		}
	}
	if author == "" {
		author = "Unknown"
	}

	safeAuthor := sanitizePath(author, 80)
	safeTitle := sanitizePath(title, 80)

	destDir, err := joinUnder(o.cfg.EbookDir, filepath.Join(safeAuthor, safeTitle))
	if err != nil {
		return filePath, err
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return filePath, err
	}

	destPath := filepath.Join(destDir, filepath.Base(filePath))
	if err := o.placeFile(filePath, destPath); err != nil {
		return filePath, err
	}

	slog.Info("ebook organized", "title", title, "dest", destPath, "mode", o.importMode())

	// Also copy to Kavita ebook library if configured.
	if o.cfg.KavitaLibraryPath != "" {
		kavitaDir, err := joinUnder(o.cfg.KavitaLibraryPath, filepath.Join(safeAuthor, safeTitle))
		if err == nil && os.MkdirAll(kavitaDir, 0755) == nil {
			kavitaDest := filepath.Join(kavitaDir, filepath.Base(destPath))
			if err := copyFileForOrg(destPath, kavitaDest); err != nil {
				slog.Warn("copy to kavita ebook library failed", "error", err)
			} else {
				slog.Info("copied to kavita ebook library", "path", kavitaDest)
			}
		}
	}

	return destPath, nil
}

// OrganizeAudiobook moves audiobook files into the organized directory structure: {AudiobookDir}/{Author}/{Title}/
func (o *Organizer) OrganizeAudiobook(filePath, title, author string) (string, error) {
	if !o.cfg.FileOrgEnabled {
		return filePath, nil
	}

	if author == "" {
		author = "Unknown"
	}

	// If source is a directory, move its contents.
	info, err := os.Lstat(filePath)
	if err != nil {
		return filePath, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return filePath, fmt.Errorf("refusing to organize symlink source %q", filePath)
	}

	safeAuthor := sanitizePath(author, 80)
	safeTitle := sanitizePath(title, 80)

	destDir, err := joinUnder(o.cfg.AudiobookDir, filepath.Join(safeAuthor, safeTitle))
	if err != nil {
		return filePath, err
	}
	if info.IsDir() {
		if err := o.placeDirTree(filePath, destDir); err != nil {
			return filePath, err
		}
		return destDir, nil
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return filePath, err
	}

	destPath := filepath.Join(destDir, filepath.Base(filePath))
	if err := o.placeFile(filePath, destPath); err != nil {
		return filePath, err
	}

	return destPath, nil
}

// OrganizeManga moves manga into {MangaDir}/{Series}/[{Volume NN}/]{file}.
// Also copies to KAVITA_MANGA_LIBRARY_PATH if configured.
func (o *Organizer) OrganizeManga(filePath, seriesTitle string) (string, error) {
	if !o.cfg.FileOrgEnabled {
		return filePath, nil
	}

	safeTitle := cleanSeriesTitle(seriesTitle)
	seriesDir, err := joinUnder(o.cfg.MangaDir, safeTitle)
	if err != nil {
		return filePath, err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return filePath, err
	}

	var resultPath string
	if info.IsDir() {
		entries, err := os.ReadDir(filePath)
		if err != nil {
			return filePath, err
		}
		for _, entry := range entries {
			src := filepath.Join(filePath, entry.Name())
			entryDir, err := mangaSeriesDestDir(seriesDir, src)
			if err != nil {
				return filePath, err
			}
			if err := os.MkdirAll(entryDir, 0755); err != nil {
				return filePath, err
			}
			dst := filepath.Join(entryDir, cleanMangaFilename(src))
			_ = o.placeFile(src, dst)
		}
		// Only a move consumes the download; hardlink/copy must leave the
		// source directory intact so the torrent can keep seeding.
		if !o.KeepsPayload() {
			_ = os.RemoveAll(filePath)
		}
		resultPath = seriesDir
	} else {
		destDir, err := mangaSeriesDestDir(seriesDir, filePath)
		if err != nil {
			return filePath, err
		}
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return filePath, err
		}
		destPath := filepath.Join(destDir, cleanMangaFilename(filePath))
		if err := o.placeFile(filePath, destPath); err != nil {
			return filePath, err
		}
		resultPath = destPath
	}

	o.mirrorMangaToKavita(safeTitle, seriesDir, resultPath)

	return resultPath, nil
}

func mangaSeriesDestDir(seriesDir, filePath string) (string, error) {
	if sub := mangaVolumeDirName(filePath); sub != "" {
		return joinUnder(seriesDir, sub)
	}
	return seriesDir, nil
}

func mangaVolumeDirName(filePath string) string {
	kind, seq, ok := releases.ParseMangaReleaseText(filepath.Base(filePath))
	if !ok || kind != models.ReleaseKindVolume || seq <= 0 {
		return ""
	}
	if seq == float64(int(seq)) {
		return sanitizePath(fmt.Sprintf("Volume %02.0f", seq), 80)
	}
	return sanitizePath(fmt.Sprintf("Volume %g", seq), 80)
}

func (o *Organizer) mirrorMangaToKavita(safeTitle, seriesDir, resultPath string) {
	if o.cfg.KavitaMangaLibraryPath == "" {
		return
	}
	kavitaSeries, err := joinUnder(o.cfg.KavitaMangaLibraryPath, safeTitle)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(seriesDir, resultPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(resultPath)
	}
	kavitaDest := filepath.Join(kavitaSeries, rel)
	if err := os.MkdirAll(filepath.Dir(kavitaDest), 0755); err != nil {
		return
	}
	resultInfo, err := os.Stat(resultPath)
	if err != nil {
		return
	}
	if resultInfo.IsDir() {
		entries, _ := os.ReadDir(resultPath)
		for _, entry := range entries {
			src := filepath.Join(resultPath, entry.Name())
			dst := filepath.Join(kavitaDest, entry.Name())
			_ = copyFileForOrg(src, dst)
		}
	} else {
		_ = copyFileForOrg(resultPath, kavitaDest)
	}
	slog.Info("copied to kavita manga library", "path", kavitaDest)
}

var (
	unsafePathRe = regexp.MustCompile(`[<>:"/\\|?*]`)
	whitespaceRe = regexp.MustCompile(`\s+`)
	bracketRe    = regexp.MustCompile(`\[[^\]]*\]`)
	parenTagsRe  = regexp.MustCompile(`\((?i:Digital|f|c2c|Viz|Complete)\)`)
	yearRe       = regexp.MustCompile(`(?i)\s*\((?:19|20)\d{2}(?:\s*-\s*(?:19|20)?\d{2})?\).*$`)
	yearOnlyRe   = regexp.MustCompile(`(?i)\s*\((?:19|20)\d{2}(?:\s*-\s*(?:19|20)?\d{2})?\)`)
	releaseTagRe = regexp.MustCompile(`(?i)\s+\([a-z0-9][a-z0-9_-]{2,}\)$`)
	volumeRe     = regexp.MustCompile(`(?i)\s*(?:Vol\.?|Volume|v)\s*\d+.*$`)
	rangeRe      = regexp.MustCompile(`\s*\d+-\d+.*$`)
)

func sanitizePath(name string, maxLen int) string {
	name = unsafePathRe.ReplaceAllString(name, "")
	name = whitespaceRe.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if len(name) > maxLen {
		name = strings.TrimSpace(name[:maxLen])
	}
	if name == "" {
		name = "Unknown"
	}
	return name
}

func cleanSeriesTitle(name string) string {
	// Strip file extensions.
	name = regexp.MustCompile(`(?i)\.(epub|cbz|cbr|pdf|zip|mobi|azw3)$`).ReplaceAllString(name, "")
	name = bracketRe.ReplaceAllString(name, "")
	name = yearRe.ReplaceAllString(name, "")
	name = parenTagsRe.ReplaceAllString(name, "")
	name = volumeRe.ReplaceAllString(name, "")
	name = rangeRe.ReplaceAllString(name, "")
	name = whitespaceRe.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, "-")
	name = strings.TrimSpace(name)
	// A series title becomes a directory name, so it must go through the same
	// separator/dot stripping every other organizer uses. Titles come from
	// manual-import requests and torrent names, both attacker-influenced.
	return sanitizePath(name, 120)
}

func cleanMangaFilename(filePath string) string {
	ext := filepath.Ext(filePath)
	name := strings.TrimSuffix(filepath.Base(filePath), ext)
	name = bracketRe.ReplaceAllString(name, "")
	name = yearOnlyRe.ReplaceAllString(name, "")
	name = parenTagsRe.ReplaceAllString(name, "")
	name = releaseTagRe.ReplaceAllString(name, "")
	name = whitespaceRe.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	return sanitizePath(name, 180) + ext
}

// joinUnder joins name onto root and verifies the result stays inside root, so
// a crafted name can never place library files elsewhere on the filesystem.
// Symlinks in the resolved prefix are followed before comparing.
func joinUnder(root, name string) (string, error) {
	dest := filepath.Join(root, name)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
		if resolvedDest, err := resolveExistingPrefix(absDest); err == nil {
			absDest = resolvedDest
		}
	}
	rel, err := filepath.Rel(absRoot, absDest)
	if err != nil {
		return "", fmt.Errorf("destination %q is outside the library root", name)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination %q is outside the library root", name)
	}
	return dest, nil
}

// resolveExistingPrefix resolves symlinks in the longest existing ancestor of
// path and re-appends the not-yet-created remainder.
func resolveExistingPrefix(path string) (string, error) {
	remainder := ""
	current := path
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			if remainder == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, remainder), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

func moveFile(src, dst string) error {
	// Try rename first (same filesystem).
	if err := renameFile(src, dst); err == nil {
		return nil
	}

	// Rename failed (often EXDEV onto CIFS/NFS): stream copy then delete.
	// Avoid os.ReadFile — large audiobooks OOM small container mem_limits.
	if err := copyFileForOrg(src, dst); err != nil {
		return err
	}
	// Flush before removing the only complete source on cross-FS moves.
	if err := syncFile(dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// renameFile is os.Rename; tests swap it to force the streaming copy fallback.
var renameFile = os.Rename

// linkFile is os.Link; tests swap it to force the copy fallback.
var linkFile = os.Link

// hardlinkFile points dst at the same data as src, leaving src in place. A
// hardlink only works within one filesystem and not on every filesystem
// (CIFS/exFAT, some FUSE mounts), so any failure falls back to a copy — the
// import must still land, it just costs the extra disk.
func hardlinkFile(src, dst string) error {
	err := linkFile(src, dst)
	if err == nil {
		return nil
	}
	// An existing destination is not a reason to copy: replace it and relink,
	// matching the overwrite behavior of the move and copy modes.
	if errors.Is(err, fs.ErrExist) {
		if rmErr := os.Remove(dst); rmErr == nil {
			if relinkErr := linkFile(src, dst); relinkErr == nil {
				return nil
			} else {
				err = relinkErr
			}
		}
	}
	slog.Warn("hardlink failed, copying instead", "src", src, "dst", dst, "error", err)
	return copyFileForOrg(src, dst)
}

// placeDirTree recreates srcDir's file tree under dstDir using the configured
// import mode, removing srcDir only when that mode is a move.
func (o *Organizer) placeDirTree(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == srcDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		return o.placeFile(path, dstPath)
	})
	if err != nil {
		return err
	}

	if o.KeepsPayload() {
		return nil
	}
	return os.RemoveAll(srcDir)
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// copyFileForOrg copies a file without removing the source (streaming).
func copyFileForOrg(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	return copyReaderToFile(srcFile, dst)
}

// copyReaderToFile streams r into dst; removes a partial dst on failure.
func copyReaderToFile(r io.Reader, dst string) error {
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = dstFile.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(dstFile, r); err != nil {
		return err
	}
	ok = true
	return nil
}
