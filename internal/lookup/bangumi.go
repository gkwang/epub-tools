package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Bangumi searches api.bgm.tv (type 1 = book) for zh and ja books.
type Bangumi struct {
	BaseURL string
	HTTP    *http.Client
}

// NewBangumi returns a Bangumi source. baseURL "" means https://api.bgm.tv.
func NewBangumi(baseURL string, hc *http.Client) *Bangumi {
	if baseURL == "" {
		baseURL = "https://api.bgm.tv"
	}
	if hc == nil {
		hc = defaultClient()
	}
	return &Bangumi{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: hc}
}

func (b *Bangumi) Name() string { return "bangumi" }

var bangumiTagRe = regexp.MustCompile(`\s*[<＜][^<>＜＞]*[>＞]\s*$`)

func (b *Bangumi) Search(ctx context.Context, q Query) ([]Candidate, error) {
	base := baseLang(q.Lang)
	if (base != "zh" && base != "ja") || strings.TrimSpace(q.Title) == "" {
		return nil, nil
	}
	kw := q.Title
	if base == "zh" {
		// Bangumi's name_cn is Simplified; search with Simplified.
		if s, ok := toSimplified(kw); ok {
			kw = s
		}
	}
	body, _ := json.Marshal(map[string]any{
		"keyword": kw,
		"filter":  map[string]any{"type": []int{1}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/v0/search/subjects?limit=10", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data []struct {
			Name   string `json:"name"`
			NameCN string `json:"name_cn"`
		} `json:"data"`
	}
	if err := doJSON(b.HTTP, req, &out); err != nil {
		return nil, fmt.Errorf("bangumi: %w", err)
	}
	var cands []Candidate
	for _, d := range out.Data {
		title, lang := d.NameCN, "zh"
		if base == "ja" {
			title, lang = bangumiTagRe.ReplaceAllString(d.Name, ""), "ja"
		}
		if strings.TrimSpace(title) == "" {
			continue
		}
		cands = append(cands, Candidate{Title: strings.TrimSpace(title), Lang: lang})
	}
	return cands, nil
}
