package lookup

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	titleThreshold = 0.85
	isbnThreshold  = 0.5
)

var (
	cnVolRe   = regexp.MustCompile(`第\s*[0-9０-９一二三四五六七八九十百零〇两兩壹貳參叁肆伍陸柒捌玖拾]+\s*[卷巻册冊集話话部]`)
	latVolRe  = regexp.MustCompile(`(?i)(?:^|[\s,:(（])(vol(?:ume)?\.?\s*\d+)`)
	trailHalf = regexp.MustCompile(`[\s　]+[上中下](?:[卷巻册冊])?$`)
	basePunct = " \t　,:：;-–—(（"
)

// splitVolume splits "Title 第一卷 Sub" into ("Title", "第一卷 Sub"). The suffix
// is "" when no volume marker is found after a non-empty base.
func splitVolume(title string) (base, suffix string) {
	title = strings.TrimSpace(title)
	idx := -1
	consider := func(i int) {
		if i > 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if m := cnVolRe.FindStringIndex(title); m != nil {
		consider(m[0])
	}
	if m := latVolRe.FindStringSubmatchIndex(title); m != nil {
		consider(m[2])
	}
	if m := trailHalf.FindStringIndex(title); m != nil {
		consider(m[0])
	}
	if idx < 0 {
		return title, ""
	}
	base = strings.TrimRight(title[:idx], basePunct)
	if base == "" {
		return title, ""
	}
	return base, strings.TrimSpace(title[idx:])
}

// foldTitle normalizes for comparison: NFKC, casefold, and only letters,
// digits and marks kept. Chinese is compared as Simplified.
func foldTitle(s, lang string) []rune {
	if baseLang(lang) == "zh" {
		if t, ok := toSimplified(s); ok {
			s = t
		}
	}
	s = cases.Fold().String(norm.NFKC.String(s))
	var out []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			out = append(out, r)
		}
	}
	return out
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// similarity is 1 - normalized edit distance on folded titles.
func similarity(a, b, lang string) float64 {
	ra, rb := foldTitle(a, lang), foldTitle(b, lang)
	n := max(len(ra), len(rb))
	if n == 0 {
		return 0
	}
	return 1 - float64(levenshtein(ra, rb))/float64(n)
}

// mainPart returns the text before a subtitle separator, or "".
func mainPart(s string) string {
	best := -1
	for _, sep := range []string{":", "：", " - ", " – ", " — "} {
		if i := strings.Index(s, sep); i > 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return strings.TrimSpace(s[:best])
}

// bestSimilarity is the best similarity between a and b, also trying the
// part before a subtitle separator on either side.
func bestSimilarity(a, b, lang string) float64 {
	best := similarity(a, b, lang)
	pa, pb := mainPart(a), mainPart(b)
	if pa != "" {
		best = max(best, similarity(pa, b, lang))
	}
	if pb != "" {
		best = max(best, similarity(a, pb, lang))
	}
	if pa != "" && pb != "" {
		best = max(best, similarity(pa, pb, lang))
	}
	return best
}

func isCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

func nameTokens(s string) []string {
	return strings.FieldsFunc(cases.Fold().String(norm.NFKC.String(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r)
	})
}

// authorSurname returns the surname of "Last, First" or "First Last".
func authorSurname(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, ","); i > 0 {
		name = name[:i]
	}
	f := strings.Fields(name)
	if len(f) == 0 {
		return name
	}
	return f[len(f)-1]
}

func authorPairMatches(a, b string) bool {
	ca, cb := isCJK(a), isCJK(b)
	if ca != cb {
		return false
	}
	if ca {
		fa, fb := string(foldTitle(a, "zh")), string(foldTitle(b, "zh"))
		if len([]rune(fa)) < 2 || len([]rune(fb)) < 2 {
			return fa != "" && fa == fb
		}
		return strings.Contains(fa, fb) || strings.Contains(fb, fa)
	}
	sa := nameTokens(authorSurname(a))
	sb := nameTokens(authorSurname(b))
	ta, tb := nameTokens(a), nameTokens(b)
	has := func(list []string, w string) bool {
		for _, x := range list {
			if x == w && len([]rune(w)) >= 2 {
				return true
			}
		}
		return false
	}
	for _, w := range sa {
		if has(tb, w) {
			return true
		}
	}
	for _, w := range sb {
		if has(ta, w) {
			return true
		}
	}
	return false
}

// authorsOverlap is true if either side is empty (nothing to compare) or
// some author of a matches some author of b.
func authorsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if authorPairMatches(x, y) {
				return true
			}
		}
	}
	return false
}
