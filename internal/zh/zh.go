// Package zh converts Chinese text between Simplified and Traditional script
// using OpenCC dictionaries embedded in the binary (no files or network).
package zh

import (
	"strings"
	"sync"

	"github.com/longbridgeapp/opencc"
)

// Converters are created lazily and used under a mutex.
var (
	mu    sync.Mutex
	cache = map[string]*opencc.OpenCC{}
	fail  = map[string]bool{}
)

func convert(cfg, s string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	if fail[cfg] {
		return s, false
	}
	c := cache[cfg]
	if c == nil {
		var err error
		c, err = opencc.New(cfg)
		if err != nil {
			fail[cfg] = true
			return s, false
		}
		cache[cfg] = c
	}
	out, err := c.Convert(s)
	if err != nil {
		return s, false
	}
	return out, true
}

// ToSimplified converts s to Simplified Chinese. ok is false if the
// converter is unavailable, in which case s is returned unchanged.
func ToSimplified(s string) (string, bool) { return convert("t2s", s) }

// ToTraditional converts s to Traditional Chinese, character by character
// (no vocabulary substitution, so titles keep their wording). Hong Kong and
// Macau tags get Hong Kong forms; everything else gets Taiwan-standard forms.
func ToTraditional(s, tag string) (string, bool) {
	cfg := "s2tw"
	if r := region(tag); r == "hk" || r == "mo" {
		cfg = "s2hk"
	}
	return convert(cfg, s)
}

// IsChinese reports whether tag's primary language is Chinese.
func IsChinese(tag string) bool { return Base(tag) == "zh" }

// IsTraditional reports whether tag denotes Traditional Chinese.
func IsTraditional(tag string) bool {
	if !IsChinese(tag) {
		return false
	}
	for _, p := range subtags(tag)[1:] {
		switch p {
		case "tw", "hk", "mo", "hant":
			return true
		}
	}
	return false
}

// Base returns the lower-case primary subtag of a language tag.
func Base(tag string) string { return subtags(tag)[0] }

func region(tag string) string {
	for _, p := range subtags(tag)[1:] {
		if len(p) == 2 {
			return p
		}
	}
	return ""
}

func subtags(tag string) []string {
	t := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(tag, "_", "-")))
	return strings.Split(t, "-")
}
