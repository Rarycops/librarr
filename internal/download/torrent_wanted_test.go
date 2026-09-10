package download

import (
	"fmt"
	"testing"

	"github.com/JeremiahM37/librarr/internal/config"
)

type wantedTorrentClient struct{}

func (wantedTorrentClient) AddTorrent(string, string, string, string, string) error { return nil }
func (wantedTorrentClient) GetTorrents(category string) ([]TorrentInfo, error) {
	return []TorrentInfo{{Name: "Look Back (2022) (Digital)", Hash: "ABC123", Category: category}}, nil
}
func (wantedTorrentClient) GetTorrentFiles(string) ([]TorrentFile, error) { return nil, nil }
func (wantedTorrentClient) DeleteTorrent(string, bool) error              { return nil }
func (wantedTorrentClient) Diagnose() map[string]interface{}              { return nil }
func (wantedTorrentClient) Name() string                                  { return "test" }

type duplicateWantedTorrentClient struct{ wantedTorrentClient }

func (duplicateWantedTorrentClient) AddTorrent(string, string, string, string, string) error {
	return fmt.Errorf("add torrent HTTP 409: Conflict")
}

func TestStartTorrentDownloadRefFindsAcceptedHash(t *testing.T) {
	manager := &Manager{
		cfg:     &config.Config{},
		torrent: wantedTorrentClient{},
	}

	ref, err := manager.StartTorrentDownloadRef(
		"https://example.org/look-back.torrent",
		"Look Back",
		"/manga-incoming",
		"manga",
		"stale-search-hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "torrent:abc123" {
		t.Fatalf("wanted ref = %q, want torrent:abc123", ref)
	}
}

func TestStartTorrentDownloadRefAcceptsExistingDuplicate(t *testing.T) {
	manager := &Manager{
		cfg:     &config.Config{},
		torrent: duplicateWantedTorrentClient{},
	}

	ref, err := manager.StartTorrentDownloadRef(
		"https://example.org/look-back.torrent",
		"Look Back",
		"/manga-incoming",
		"manga",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "torrent:abc123" {
		t.Fatalf("wanted ref = %q, want torrent:abc123", ref)
	}
}
