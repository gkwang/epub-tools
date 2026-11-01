package lookup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// GoogleBooks searches the Google Books volumes API.
type GoogleBooks struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewGoogleBooks returns a Google Books source. baseURL "" means
// https://www.googleapis.com; apiKey "" means anonymous access.
func NewGoogleBooks(baseURL, apiKey string, hc *http.Client) *GoogleBooks {
	if baseURL == "" {
		baseURL = "https://www.googleapis.com"
	}
	if hc == nil {
		hc = defaultClient()
	}
	return &GoogleBooks{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: hc}
}

func (g *GoogleBooks) Name() string { return "google" }

func (g *GoogleBooks) query(ctx context.Context, q string) ([]Candidate, error) {
	v := url.Values{}
	v.Set("q", q)
	v.Set("maxResults", "10")
	v.Set("printType", "books")
	if g.APIKey != "" {
		v.Set("key", g.APIKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+"/books/v1/volumes?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []struct {
			VolumeInfo struct {
				Title     string   `json:"title"`
				Authors   []string `json:"authors"`
				Language  string   `json:"language"`
				Publisher string   `json:"publisher"`
				IDs       []struct {
					Type       string `json:"type"`
					Identifier string `json:"identifier"`
				} `json:"industryIdentifiers"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	if err := doJSON(g.HTTP, req, &out); err != nil {
		return nil, fmt.Errorf("google: %w", err)
	}
	var cands []Candidate
	for _, it := range out.Items {
		vi := it.VolumeInfo
		if strings.TrimSpace(vi.Title) == "" {
			continue
		}
		var isbns []string
		isbn := ""
		for _, id := range vi.IDs {
			if id.Type != "ISBN_13" && id.Type != "ISBN_10" {
				continue
			}
			isbns = append(isbns, id.Identifier)
			if isbn == "" || id.Type == "ISBN_13" {
				isbn = id.Identifier
			}
		}
		cands = append(cands, Candidate{
			Title: vi.Title, Lang: vi.Language, Authors: vi.Authors,
			ISBN: isbn, Publisher: vi.Publisher,
			Taiwan: IsTaiwanEdition(vi.Publisher, isbns...),
		})
	}
	return cands, nil
}

// Search tries an ISBN query first, then intitle:/inauthor:. An error from the
// first query is returned as is; an error from the second is ignored if the
// first produced candidates.
func (g *GoogleBooks) Search(ctx context.Context, q Query) ([]Candidate, error) {
	var all []Candidate
	var firstErr error
	if q.ISBN != "" {
		c, err := g.query(ctx, "isbn:"+q.ISBN)
		if err != nil {
			return nil, err
		}
		for i := range c {
			c[i].ISBNMatch = true
		}
		all = c
	}
	if strings.TrimSpace(q.Title) != "" {
		s := "intitle:" + q.Title
		if len(q.Authors) > 0 {
			s += " inauthor:" + authorSurname(q.Authors[0])
		}
		c, err := g.query(ctx, s)
		if err != nil {
			firstErr = err
		}
		all = append(all, c...)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

// GoogleAPIKeyFromEnv returns GOOGLE_BOOKS_API_KEY.
func googleAPIKeyFromEnv() string { return os.Getenv("GOOGLE_BOOKS_API_KEY") }
