package lookup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenLibrary searches openlibrary.org/search.json.
type OpenLibrary struct {
	BaseURL string
	HTTP    *http.Client
	lim     *limiter
}

// NewOpenLibrary returns an Open Library source. baseURL "" means
// https://openlibrary.org; minInterval is the minimum gap between requests
// (the production value is one second).
func NewOpenLibrary(baseURL string, hc *http.Client, minInterval time.Duration) *OpenLibrary {
	if baseURL == "" {
		baseURL = "https://openlibrary.org"
	}
	if hc == nil {
		hc = defaultClient()
	}
	return &OpenLibrary{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: hc, lim: &limiter{min: minInterval}}
}

func (o *OpenLibrary) Name() string { return "openlibrary" }

func (o *OpenLibrary) query(ctx context.Context, v url.Values) ([]Candidate, error) {
	v.Set("fields", "title,author_name,language")
	v.Set("limit", "10")
	if err := o.lim.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/search.json?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Docs []struct {
			Title    string   `json:"title"`
			Authors  []string `json:"author_name"`
			Language []string `json:"language"`
		} `json:"docs"`
	}
	if err := doJSON(o.HTTP, req, &out); err != nil {
		return nil, fmt.Errorf("openlibrary: %w", err)
	}
	var cands []Candidate
	for _, d := range out.Docs {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		if len(d.Language) == 0 {
			cands = append(cands, Candidate{Title: d.Title, Authors: d.Authors})
			continue
		}
		// A work lists the languages of all its editions; offer one
		// candidate per language and let the language check pick.
		for _, l := range d.Language {
			cands = append(cands, Candidate{Title: d.Title, Authors: d.Authors, Lang: marcToISO(l)})
		}
	}
	return cands, nil
}

func (o *OpenLibrary) Search(ctx context.Context, q Query) ([]Candidate, error) {
	var all []Candidate
	var firstErr error
	if q.ISBN != "" {
		c, err := o.query(ctx, url.Values{"isbn": {q.ISBN}})
		if err != nil {
			return nil, err
		}
		for i := range c {
			c[i].ISBNMatch = true
		}
		all = c
	}
	if strings.TrimSpace(q.Title) != "" {
		v := url.Values{"title": {q.Title}}
		if len(q.Authors) > 0 {
			v.Set("author", authorSurname(q.Authors[0]))
		}
		c, err := o.query(ctx, v)
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

var marcToISO639 = map[string]string{
	"eng": "en", "fre": "fr", "fra": "fr", "ger": "de", "deu": "de", "spa": "es",
	"ita": "it", "por": "pt", "dut": "nl", "nld": "nl", "chi": "zh", "zho": "zh",
	"jpn": "ja", "kor": "ko", "rus": "ru", "swe": "sv", "dan": "da", "nor": "no",
	"fin": "fi", "pol": "pl", "cze": "cs", "ces": "cs", "gre": "el", "ell": "el",
	"heb": "he", "ara": "ar", "tur": "tr", "hun": "hu", "lat": "la", "ukr": "uk",
	"hin": "hi", "tha": "th", "vie": "vi", "ind": "id", "cat": "ca", "rum": "ro",
	"ron": "ro", "per": "fa", "fas": "fa", "bul": "bg", "hrv": "hr", "slv": "sl",
	"slo": "sk", "slk": "sk", "ice": "is", "isl": "is", "wel": "cy", "cym": "cy",
}

// marcToISO converts a MARC language code to ISO 639-1. Unknown codes are
// returned lower-cased so they can never equal a real two-letter code.
func marcToISO(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if v, ok := marcToISO639[code]; ok {
		return v
	}
	return code
}
