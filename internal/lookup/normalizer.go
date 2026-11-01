package lookup

import (
	"context"
	"strings"
	"time"

	"github.com/gkwang/epub-tools/internal/epub"
)

// Options configures a Normalizer.
type Options struct {
	// PreferTaiwan makes Chinese (zh) books prefer an accepted Taiwan-edition
	// catalog match over any other accepted match, even from a later source.
	PreferTaiwan bool
}

type normalizer struct {
	opts    Options
	sources []Source
}

// New returns the production Normalizer: Bangumi (zh/ja only), Google Books
// (optional GOOGLE_BOOKS_API_KEY) and Open Library, in that order, with a
// 10 second HTTP timeout and at most one Open Library request per second.
func New(opts Options) Normalizer {
	hc := defaultClient()
	return NewWithSources(opts,
		NewBangumi("", hc),
		NewGoogleBooks("", googleAPIKeyFromEnv(), hc),
		NewOpenLibrary("", hc, time.Second),
	)
}

// NewWithSources returns a Normalizer that consults sources in order. Tests
// use it with sources pointed at httptest servers.
func NewWithSources(opts Options, sources ...Source) Normalizer {
	return &normalizer{opts: opts, sources: sources}
}

func embedded(md epub.Metadata, warn string) Result {
	return Result{Title: md.Title, Source: "embedded", Warn: warn}
}

func (n *normalizer) Normalize(ctx context.Context, md epub.Metadata) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = embedded(md, "lookup failed unexpectedly")
		}
	}()
	if strings.TrimSpace(md.Title) == "" || strings.TrimSpace(md.Lang) == "" {
		return embedded(md, "")
	}
	trad := isTraditional(md.Lang)
	if trad {
		if _, ok := toTraditional("测试", md.Lang); !ok {
			return embedded(md, "lookup skipped: Chinese script conversion unavailable for Traditional Chinese")
		}
	}

	base, vol := splitVolume(md.Title)
	q := Query{Title: base, Authors: md.Authors, ISBN: md.ISBN, Lang: md.Lang}

	prefer := n.opts.PreferTaiwan && baseLang(md.Lang) == "zh"
	var fallback *Result
	var warns []string
	for _, src := range n.sources {
		if ctx.Err() != nil {
			warns = append(warns, ctx.Err().Error())
			break
		}
		cands, err := src.Search(ctx, q)
		if err != nil {
			warns = append(warns, err.Error())
			continue
		}
		for _, c := range cands {
			title, ok := accept(md, base, vol, c, trad)
			if !ok {
				continue
			}
			r := Result{Title: title, Source: src.Name()}
			if c.Taiwan && baseLang(md.Lang) == "zh" {
				r.Edition = "taiwan"
			}
			if !prefer || r.Edition == "taiwan" {
				return r
			}
			if fallback == nil {
				fallback = &r
			}
		}
	}
	if fallback != nil {
		return *fallback
	}
	return embedded(md, strings.Join(warns, "; "))
}

// accept decides whether candidate c is a trustworthy canonical title for md
// and returns the final title.
func accept(md epub.Metadata, base, vol string, c Candidate, trad bool) (string, bool) {
	if strings.TrimSpace(c.Title) == "" {
		return "", false
	}
	cBase, cVol := splitVolume(c.Title)
	if vol == "" && cVol != "" {
		return "", false // catalog entry is a specific volume of something else
	}
	sim := bestSimilarity(base, cBase, md.Lang)
	switch {
	case c.Lang != "" && baseLang(c.Lang) != baseLang(md.Lang):
		return "", false
	case c.Lang == "" && !(c.ISBNMatch && sim >= titleThreshold):
		// No language from the catalog: only an exact ISBN hit with a
		// near-identical title is trustworthy enough.
		return "", false
	}
	if c.ISBNMatch {
		if sim < isbnThreshold {
			return "", false
		}
	} else {
		if sim < titleThreshold || !authorsOverlap(md.Authors, c.Authors) {
			return "", false
		}
	}

	title := strings.TrimSpace(c.Title)
	if baseLang(md.Lang) == "zh" {
		if c.Taiwan && trad {
			// Taiwan titles are already Traditional; only convert if the
			// text has no Traditional-only characters (i.e. came back Simplified).
			if s, ok := toSimplified(title); ok && s == title {
				if t, ok := toTraditional(title, md.Lang); ok {
					title = t
				}
			}
		} else if trad {
			if t, ok := toTraditional(title, md.Lang); ok {
				title = t
			}
		} else if t, ok := toSimplified(title); ok {
			title = t
		}
	}
	title = fixCasing(title, md.Lang)
	if cVol == "" && vol != "" {
		title += " " + vol
	}
	return title, true
}
