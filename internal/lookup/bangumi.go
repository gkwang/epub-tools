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
			Name    string `json:"name"`
			NameCN  string `json:"name_cn"`
			Infobox []struct {
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			} `json:"infobox"`
		} `json:"data"`
	}
	if err := doJSON(b.HTTP, req, &out); err != nil {
		return nil, fmt.Errorf("bangumi: %w", err)
	}
	var cands []Candidate
	for _, d := range out.Data {
		title, lang := d.NameCN, "zh"
		var pub, isbn string
		if base == "zh" {
			pub, isbn = bangumiInfobox(d.Infobox)
		}
		if tw := base == "zh" && IsTaiwanEdition(pub, isbn); tw && strings.TrimSpace(d.Name) != "" {
			// For Taiwan editions name is the Traditional Chinese title.
			cands = append(cands, Candidate{Title: strings.TrimSpace(d.Name), Lang: "zh-TW",
				ISBN: isbn, Publisher: pub, Taiwan: true})
			continue
		}
		if base == "ja" {
			title, lang = bangumiTagRe.ReplaceAllString(d.Name, ""), "ja"
		}
		if strings.TrimSpace(title) == "" {
			continue
		}
		cands = append(cands, Candidate{Title: strings.TrimSpace(title), Lang: lang, ISBN: isbn, Publisher: pub})
	}
	return cands, nil
}

// bangumiInfobox extracts 出版社 (joined with "、") and the first ISBN from
// an infobox. Values are a string or a list of {"v": string}.
func bangumiInfobox(box []struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}) (publisher, isbn string) {
	var pubs []string
	for _, e := range box {
		var vals []string
		var one string
		var list []struct {
			V string `json:"v"`
		}
		if json.Unmarshal(e.Value, &one) == nil {
			vals = []string{one}
		} else if json.Unmarshal(e.Value, &list) == nil {
			for _, x := range list {
				vals = append(vals, x.V)
			}
		}
		switch e.Key {
		case "出版社":
			pubs = append(pubs, vals...)
		case "ISBN":
			if isbn == "" && len(vals) > 0 {
				isbn = strings.TrimSpace(vals[0])
			}
		}
	}
	return strings.Join(pubs, "、"), isbn
}
