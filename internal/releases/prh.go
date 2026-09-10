package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

const defaultPRHBaseURL = "https://api.penguinrandomhouse.com/resources/v2/title"

// PRHProvider reads forthcoming Kodansha/PRH US catalog rows through the
// Enhanced PRH API.
type PRHProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	now     func() time.Time
}

// NewPRHProvider builds a PRH catalog client. baseURL may be empty to use the
// public API host; tests point it at an httptest server.
func NewPRHProvider(client *http.Client, apiKey string, now func() time.Time) *PRHProvider {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &PRHProvider{
		client:  client,
		apiKey:  apiKey,
		baseURL: defaultPRHBaseURL,
		now:     now,
	}
}

// WithBaseURL overrides the API host for tests.
func (p *PRHProvider) WithBaseURL(baseURL string) *PRHProvider {
	p.baseURL = strings.TrimRight(baseURL, "/")
	return p
}

func (p *PRHProvider) Name() string { return "prh" }

// SearchSeries queries PRH for forthcoming releases matching a series name.
func (p *PRHProvider) SearchSeries(ctx context.Context, seriesName, author string) ([]models.MangaRelease, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("PRH API key is not configured")
	}
	query := strings.TrimSpace(seriesName)
	if query == "" {
		return nil, fmt.Errorf("series name is required")
	}

	u, err := url.Parse(p.baseURL + "/domains/PRH.US/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("api_key", p.apiKey)
	q.Set("q", query)
	q.Set("rows", "100")
	q.Set("forthcomingRelease", "true")
	if strings.TrimSpace(author) != "" {
		q.Set("author", strings.TrimSpace(author))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("PRH search failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	now := p.now().UTC()
	var releases []models.MangaRelease
	var skipped int
	for _, raw := range payload.Results {
		release, ok := parsePRHRow(raw, seriesName, now)
		if !ok {
			skipped++
			continue
		}
		releases = append(releases, release)
	}
	if len(payload.Results) > 0 && len(releases) == 0 && skipped > 0 {
		return nil, fmt.Errorf("PRH returned %d rows but none were usable", len(payload.Results))
	}
	return releases, nil
}

func parsePRHRow(raw json.RawMessage, wantSeries string, fetchedAt time.Time) (models.MangaRelease, bool) {
	var row struct {
		ISBN         string          `json:"isbn"`
		Key          string          `json:"key"`
		Name         string          `json:"name"`
		Title        string          `json:"title"`
		SeriesName   string          `json:"seriesName"`
		SeriesNumber json.Number     `json:"seriesNumber"`
		Author       json.RawMessage `json:"author"`
		OnSaleDate   string          `json:"onSaleDate"`
		OnSale       string          `json:"onsale"`
		Description  json.RawMessage `json:"description"`
		URL          string          `json:"url"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return models.MangaRelease{}, false
	}

	isbn := strings.TrimSpace(firstNonEmpty(row.ISBN, row.Key))
	if isbn == "" || isbn == "not-a-release" {
		return models.MangaRelease{}, false
	}

	title := strings.TrimSpace(firstNonEmpty(row.Name, row.Title))
	if title == "" {
		return models.MangaRelease{}, false
	}

	seriesName := strings.TrimSpace(firstNonEmpty(row.SeriesName, wantSeries))
	if seriesName == "" || !seriesNamesMatch(seriesName, wantSeries) {
		return models.MangaRelease{}, false
	}

	onSale, ok := parsePRHDate(firstNonEmpty(row.OnSaleDate, row.OnSale))
	if !ok {
		return models.MangaRelease{}, false
	}

	kind := models.ReleaseKindVolume
	sequence := 0.0
	if row.SeriesNumber != "" {
		if n, err := row.SeriesNumber.Float64(); err == nil && n > 0 {
			sequence = n
		}
	}
	if parsedKind, parsedSeq, parsedOK := ParseMangaReleaseText(title); parsedOK {
		kind = parsedKind
		if sequence == 0 {
			sequence = parsedSeq
		}
	} else if sequence == 0 {
		return models.MangaRelease{}, false
	}

	desc := flattenPRHText(row.Description)
	final := strings.Contains(strings.ToUpper(desc), "FINAL")

	sourceURL := strings.TrimSpace(row.URL)
	if sourceURL == "" {
		sourceURL = "https://www.penguinrandomhouse.com/books/" + isbn + "/"
	}

	return models.MangaRelease{
		Provider:    "prh",
		ProviderKey: isbn,
		SeriesName:  seriesName,
		Author:      parsePRHAuthors(row.Author),
		Title:       title,
		Kind:        kind,
		Sequence:    sequence,
		OnSaleAt:    onSale,
		Final:       final,
		SourceURL:   sourceURL,
		FetchedAt:   fetchedAt,
	}, true
}

func parsePRHAuthors(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var authors []string
	if err := json.Unmarshal(raw, &authors); err == nil {
		return cleanPRHAuthor(authors)
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return cleanPRHAuthor([]string{one})
	}
	return ""
}

func cleanPRHAuthor(authors []string) string {
	if len(authors) == 0 {
		return ""
	}
	name := strings.TrimSpace(authors[0])
	if parts := strings.SplitN(name, "|", 2); len(parts) == 2 {
		name = strings.TrimSpace(parts[1])
	}
	return name
}

func flattenPRHText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err == nil {
		return strings.Join(lines, " ")
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return one
	}
	return ""
}

func parsePRHDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02",
		"01/02/2006",
		"1/2/2006",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

func seriesNamesMatch(got, want string) bool {
	gotNorm := NormalizeSeriesName(got)
	wantNorm := NormalizeSeriesName(want)
	return gotNorm == wantNorm || strings.Contains(gotNorm, wantNorm) || strings.Contains(wantNorm, gotNorm)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ReleaseKey returns the stable wishlist key for a catalog release.
func ReleaseKey(provider, providerKey string) string {
	return provider + ":" + providerKey
}

// ParseSeriesNumber is exported for tests that assert PRH row parsing.
func ParseSeriesNumber(raw string) (float64, bool) {
	n, err := strconv.ParseFloat(raw, 64)
	return n, err == nil && n > 0
}
