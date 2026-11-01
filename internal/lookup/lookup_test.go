package lookup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gkwang/epub-tools/internal/epub"
)

type fixtures struct {
	bangumi, google, ol http.HandlerFunc
	hits                [3]atomic.Int32
}

func jsonH(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Error(w, "{}", code) }
}

func (f *fixtures) normalizer(t *testing.T) Normalizer {
	t.Helper()
	return f.normalizerOpts(t, Options{})
}

func (f *fixtures) normalizerOpts(t *testing.T, opts Options) Normalizer {
	t.Helper()
	wrap := func(i int, h http.HandlerFunc) *httptest.Server {
		if h == nil {
			h = jsonH(`{}`)
		}
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.hits[i].Add(1)
			if r.Header.Get("User-Agent") != UserAgent {
				t.Errorf("bad User-Agent %q", r.Header.Get("User-Agent"))
			}
			h(w, r)
		}))
		t.Cleanup(s.Close)
		return s
	}
	b, g, o := wrap(0, f.bangumi), wrap(1, f.google), wrap(2, f.ol)
	return NewWithSources(opts, NewBangumi(b.URL, nil), NewGoogleBooks(g.URL, "", nil), NewOpenLibrary(o.URL, nil, 0))
}

func total(f *fixtures) int32 { return f.hits[0].Load() + f.hits[1].Load() + f.hits[2].Load() }

func TestISBNHitGoogle(t *testing.T) {
	f := &fixtures{google: func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "isbn:9780063511637" {
			// title query: nothing
			io.WriteString(w, `{"totalItems":0}`)
			return
		}
		io.WriteString(w, `{"kind":"books#volumes","totalItems":1,"items":[{"volumeInfo":{"title":"Whistler","authors":["Ann Patchett"],"language":"en"}}]}`)
	}}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Patchett, Ann"}, ISBN: "9780063511637"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "google" || got.Title != "Whistler" || got.Warn != "" {
		t.Fatalf("got %+v", got)
	}
	if f.hits[0].Load() != 0 {
		t.Error("bangumi must not be queried for en")
	}
}

func TestTranslatedEditionRejected(t *testing.T) {
	f := &fixtures{
		google: jsonH(`{"items":[{"volumeInfo":{"title":"Whistler","authors":["Ann Patchett"],"language":"es"}}]}`),
		ol:     jsonH(`{"docs":[{"title":"Whistler","author_name":["Ann Patchett"],"language":["spa"]}]}`),
	}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Patchett, Ann"}, ISBN: "9780063511637"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "embedded" || got.Title != "Whistler" {
		t.Fatalf("got %+v", got)
	}
}

func TestSimilarityRejected(t *testing.T) {
	f := &fixtures{google: jsonH(`{"items":[{"volumeInfo":{"title":"Whistling Past the Graveyard","authors":["Ann Patchett"],"language":"en"}}]}`)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Patchett, Ann"}}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "embedded" {
		t.Fatalf("got %+v", got)
	}
}

func TestAuthorMismatchRejected(t *testing.T) {
	f := &fixtures{google: jsonH(`{"items":[{"volumeInfo":{"title":"Whistler","authors":["John Smith"],"language":"en"}}]}`)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Patchett, Ann"}}
	if got := f.normalizer(t).Normalize(context.Background(), md); got.Source != "embedded" {
		t.Fatalf("got %+v", got)
	}
}

func TestSubtitleAccepted(t *testing.T) {
	f := &fixtures{google: jsonH(`{"items":[{"volumeInfo":{"title":"Whistler: A Novel of Music","authors":["Ann Patchett"],"language":"en-US"}}]}`)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Ann Patchett"}}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "google" || got.Title != "Whistler: A Novel of Music" {
		t.Fatalf("got %+v", got)
	}
}

func TestBangumiVolumeReappended(t *testing.T) {
	var kw string
	f := &fixtures{bangumi: func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v0/search/subjects" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Keyword string `json:"keyword"`
			Filter  struct {
				Type []int `json:"type"`
			} `json:"filter"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		kw = body.Keyword
		if len(body.Filter.Type) != 1 || body.Filter.Type[0] != 1 {
			t.Errorf("bad filter %+v", body.Filter)
		}
		io.WriteString(w, `{"data":[{"id":426138,"name":"末日之扉","name_cn":""},{"id":90809,"name":"扉の魔術師の召喚契約<アドヴェント・ゲート>","name_cn":"门之魔术师的召唤契约","platform":"小说"}],"total":2,"limit":10,"offset":0}`)
	}}
	md := epub.Metadata{Title: "扉之魔术师的召唤契约 第一卷 拥有最强之名的少女", Lang: "zh"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Title != "门之魔术师的召唤契约 第一卷 拥有最强之名的少女" {
		t.Fatalf("got %+v", got)
	}
	if kw != "扉之魔术师的召唤契约" {
		t.Errorf("keyword %q", kw)
	}
}

func TestBangumiTraditional(t *testing.T) {
	var kw string
	f := &fixtures{bangumi: func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keyword string `json:"keyword"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		kw = body.Keyword
		io.WriteString(w, `{"data":[{"name":"優等生がアダルトグッズを買いに来た","name_cn":"来购买成人用品的优等生"}]}`)
	}}
	md := epub.Metadata{Title: "來購買成人用品的優等生 第一卷 上", Lang: "zh-TW"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Title != "來購買成人用品的優等生 第一卷 上" {
		t.Fatalf("got %+v", got)
	}
	if kw != "来购买成人用品的优等生" {
		t.Errorf("keyword should be simplified, got %q", kw)
	}
}

func TestBangumiJapanese(t *testing.T) {
	f := &fixtures{bangumi: jsonH(`{"data":[{"name":"扉の魔術師の召喚契約<アドヴェント・ゲート>","name_cn":"门之魔术师的召唤契约"}]}`)}
	md := epub.Metadata{Title: "扉の魔術師の召喚契約 第1巻", Lang: "ja"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Title != "扉の魔術師の召喚契約 第1巻" {
		t.Fatalf("got %+v", got)
	}
}

func TestOpenLibraryMARC(t *testing.T) {
	f := &fixtures{ol: jsonH(`{"numFound":1,"docs":[{"title":"Le Petit Prince","author_name":["Antoine de Saint-Exupéry"],"language":["eng","fre","ger"]}]}`)}
	md := epub.Metadata{Title: "Le petit prince", Lang: "fr", Authors: []string{"Saint-Exupéry, Antoine de"}}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "openlibrary" || got.Title != "Le Petit Prince" {
		t.Fatalf("got %+v", got)
	}
	for in, want := range map[string]string{"eng": "en", "fre": "fr", "ger": "de", "spa": "es", "ita": "it", "por": "pt", "dut": "nl", "chi": "zh", "jpn": "ja", "kor": "ko", "rus": "ru", "xxx": "xxx"} {
		if g := marcToISO(in); g != want {
			t.Errorf("marcToISO(%q)=%q want %q", in, g, want)
		}
	}
}

// Open Library often omits language; an exact ISBN hit with a matching
// title is still trusted (real response shape for this ISBN).
func TestOpenLibraryNoLanguageISBNAccepted(t *testing.T) {
	f := &fixtures{ol: jsonH(`{"docs":[{"title":"WHISTLER","author_name":["Ann Patchett"]}]}`)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", ISBN: "9780063511637"}
	if got := f.normalizer(t).Normalize(context.Background(), md); got.Source != "openlibrary" || got.Title != "Whistler" {
		t.Fatalf("got %+v", got)
	}
}

// Without an ISBN, a language-less candidate is not trusted.
func TestOpenLibraryNoLanguageTitleSearchRejected(t *testing.T) {
	f := &fixtures{ol: jsonH(`{"docs":[{"title":"Whistler","author_name":["Ann Patchett"]}]}`)}
	md := epub.Metadata{Title: "Whistler", Lang: "en"}
	if got := f.normalizer(t).Normalize(context.Background(), md); got.Source != "embedded" {
		t.Fatalf("got %+v", got)
	}
}

func TestCasing(t *testing.T) {
	cases := []struct{ in, lang, want string }{
		{"THE LORD OF THE RINGS", "en", "The Lord of the Rings"},
		{"war and peace", "en", "War and Peace"},
		{"WHAT I TALK ABOUT: THE END", "en", "What I Talk About: The End"},
		{"le petit prince", "fr", "Le petit prince"},
		{"EL SEÑOR DE LOS ANILLOS", "es", "El señor de los anillos"},
		{"DER HERR DER RINGE", "de", "DER HERR DER RINGE"},
		{"The Lord of the Rings", "en", "The Lord of the Rings"},
		{"门之魔术师", "zh", "门之魔术师"},
	}
	for _, c := range cases {
		if g := fixCasing(c.in, c.lang); g != c.want {
			t.Errorf("fixCasing(%q,%q)=%q want %q", c.in, c.lang, g, c.want)
		}
	}
	f := &fixtures{google: jsonH(`{"items":[{"volumeInfo":{"title":"THE LORD OF THE RINGS","authors":["J. R. R. Tolkien"],"language":"en"}}]}`)}
	md := epub.Metadata{Title: "The Lord of the Rings", Lang: "en", Authors: []string{"Tolkien, J. R. R."}}
	if got := f.normalizer(t).Normalize(context.Background(), md); got.Title != "The Lord of the Rings" || got.Source != "google" {
		t.Fatalf("got %+v", got)
	}
}

func TestFallbackOn429(t *testing.T) {
	f := &fixtures{google: status(429), ol: status(429)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", ISBN: "9780063511637"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "embedded" || got.Title != "Whistler" || got.Warn == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestFallThroughToNextSource(t *testing.T) {
	f := &fixtures{
		google: status(500),
		ol:     jsonH(`{"docs":[{"title":"Whistler","author_name":["Ann Patchett"],"language":["eng"]}]}`),
	}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Ann Patchett"}}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "openlibrary" || got.Warn != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestEmptyLangSkipped(t *testing.T) {
	f := &fixtures{}
	md := epub.Metadata{Title: "Whistler", ISBN: "9780063511637"}
	got := f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "embedded" || got.Title != "Whistler" || total(f) != 0 {
		t.Fatalf("got %+v hits=%d", got, total(f))
	}
	if got := f.normalizer(t).Normalize(context.Background(), epub.Metadata{Lang: "en"}); got.Title != "" || total(f) != 0 {
		t.Fatalf("empty title: %+v", got)
	}
}

func TestSplitVolume(t *testing.T) {
	cases := []struct{ in, base, suf string }{
		{"扉之魔术师的召唤契约 第一卷 拥有最强之名的少女", "扉之魔术师的召唤契约", "第一卷 拥有最强之名的少女"},
		{"來購買成人用品的優等生 第一卷 上", "來購買成人用品的優等生", "第一卷 上"},
		{"Foo Bar, Vol. 3", "Foo Bar", "Vol. 3"},
		{"某作品 第12巻", "某作品", "第12巻"},
		{"某作品 下", "某作品", "下"},
		{"Whistler", "Whistler", ""},
		{"Volume 1", "Volume 1", ""},
	}
	for _, c := range cases {
		b, s := splitVolume(c.in)
		if b != c.base || s != c.suf {
			t.Errorf("splitVolume(%q)=(%q,%q) want (%q,%q)", c.in, b, s, c.base, c.suf)
		}
	}
}

func TestAuthorsOverlap(t *testing.T) {
	yes := [][2]string{{"Patchett, Ann", "Ann Patchett"}, {"Saint-Exupéry, Antoine de", "Antoine de Saint-Exupéry"}, {"川原礫", "川原 礫"}, {"鎌池和馬", "鎌池"}}
	for _, p := range yes {
		if !authorsOverlap([]string{p[0]}, []string{p[1]}) {
			t.Errorf("%q vs %q should overlap", p[0], p[1])
		}
	}
	if authorsOverlap([]string{"Patchett, Ann"}, []string{"John Smith"}) || authorsOverlap([]string{"川原礫"}, []string{"Reki Kawahara"}) {
		t.Error("unexpected overlap")
	}
}

func TestIsTaiwanEdition(t *testing.T) {
	tests := []struct {
		pub   string
		isbns []string
		want  bool
	}{
		{"", []string{"9789571052595"}, true},
		{"", []string{"978-986-123-456-7"}, true},
		{"", []string{"9786261234567"}, true},
		{"", []string{"9571052590"}, true},
		{"", []string{"986-123-456-X"}, true},
		{"", []string{"9784088800001"}, false},
		{"", []string{"9787020000000"}, false},
		{"", []string{"9789881234567"}, false}, // Hong Kong
		{"尖端出版", nil, true},
		{"台灣東販", nil, true},
		{"臺灣角川", nil, true},
		{"某某台湾分公司", nil, true},
		{"角川書店", nil, false},
		{"KADOKAWA", nil, false},
		{"人民文学出版社", nil, false},
		{"芳文社、台灣東販", nil, true},
		{"芳文社、台灣東販", []string{"9784832280000"}, false},
		{"芳文社、台灣東販", []string{"9789863000000"}, true},
		{"", nil, false},
	}
	for _, tc := range tests {
		if got := IsTaiwanEdition(tc.pub, tc.isbns...); got != tc.want {
			t.Errorf("IsTaiwanEdition(%q, %v) = %v, want %v", tc.pub, tc.isbns, got, tc.want)
		}
	}
}

const bangumiMainlandAndTW = `{"data":[
{"id":1,"name":"来革命吧!覆盆子ja","name_cn":"来革命吧！覆盆子","infobox":[{"key":"出版社","value":"人民出版社"},{"key":"ISBN","value":"9787000000000"}]},
{"id":425958,"name":"來革命吧！覆盆子","name_cn":"来革命吧！覆盆子","infobox":[{"key":"出版社","value":[{"v":"尖端出版"}]},{"key":"ISBN","value":"9789571052595"}]}
]}`

func TestBangumiPreferTaiwan(t *testing.T) {
	md := epub.Metadata{Title: "來革命吧！覆盆子", Lang: "zh-TW"}
	f := &fixtures{bangumi: jsonH(bangumiMainlandAndTW)}
	got := f.normalizerOpts(t, Options{PreferTaiwan: true}).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Edition != "taiwan" || got.Title != "來革命吧！覆盆子" {
		t.Fatalf("got %+v", got)
	}
	f = &fixtures{bangumi: jsonH(bangumiMainlandAndTW)}
	got = f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Edition != "" || got.Title != "來革命吧！覆盆子" {
		t.Fatalf("no preference: got %+v", got)
	}
}

const googleTW = `{"items":[{"volumeInfo":{"title":"來革命吧！覆盆子","language":"zh-TW","publisher":"尖端","industryIdentifiers":[{"type":"ISBN_13","identifier":"9789571052595"}]}}]}`

func TestGoogleTaiwanBeatsEarlierBangumiMainland(t *testing.T) {
	mainland := `{"data":[{"name":"x","name_cn":"来革命吧！覆盆子","infobox":[{"key":"ISBN","value":"9787000000000"}]}]}`
	md := epub.Metadata{Title: "來革命吧！覆盆子", Lang: "zh-TW"}
	f := &fixtures{bangumi: jsonH(mainland), google: jsonH(googleTW)}
	got := f.normalizerOpts(t, Options{PreferTaiwan: true}).Normalize(context.Background(), md)
	if got.Source != "google" || got.Edition != "taiwan" || got.Title != "來革命吧！覆盆子" {
		t.Fatalf("got %+v", got)
	}
	f = &fixtures{bangumi: jsonH(mainland), google: jsonH(googleTW)}
	got = f.normalizer(t).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Edition != "" {
		t.Fatalf("no preference: got %+v", got)
	}
	if f.hits[1].Load() != 0 {
		t.Error("google should not be queried without preference")
	}
}

func TestPreferTaiwanFallback(t *testing.T) {
	mainland := `{"data":[{"name":"x","name_cn":"来革命吧！覆盆子","infobox":[{"key":"ISBN","value":"9787000000000"}]}]}`
	hk := `{"items":[{"volumeInfo":{"title":"來革命吧！覆盆子","language":"zh-TW","publisher":"某港出版","industryIdentifiers":[{"type":"ISBN_13","identifier":"9789881234567"}]}}]}`
	f := &fixtures{bangumi: jsonH(mainland), google: jsonH(hk)}
	md := epub.Metadata{Title: "來革命吧！覆盆子", Lang: "zh-TW"}
	got := f.normalizerOpts(t, Options{PreferTaiwan: true}).Normalize(context.Background(), md)
	if got.Source != "bangumi" || got.Edition != "" || got.Title != "來革命吧！覆盆子" {
		t.Fatalf("got %+v", got)
	}
	if f.hits[1].Load() == 0 {
		t.Error("google should have been consulted")
	}
}

func TestPreferTaiwanIgnoredForNonChinese(t *testing.T) {
	g := `{"items":[{"volumeInfo":{"title":"Whistler","authors":["Ann Patchett"],"language":"en","publisher":"台灣東販"}},{"volumeInfo":{"title":"Whistler","authors":["Ann Patchett"],"language":"en"}}]}`
	f := &fixtures{google: jsonH(g)}
	md := epub.Metadata{Title: "Whistler", Lang: "en", Authors: []string{"Ann Patchett"}}
	got := f.normalizerOpts(t, Options{PreferTaiwan: true}).Normalize(context.Background(), md)
	if got.Source != "google" || got.Title != "Whistler" || got.Edition != "" {
		t.Fatalf("got %+v", got)
	}
	if f.hits[2].Load() != 0 {
		t.Error("open library must not be queried once google matched")
	}
}
