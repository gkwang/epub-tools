package lookup

import (
	"strings"
	"sync"

	"github.com/longbridgeapp/opencc"
)

// OpenCC converters are created lazily and used under a mutex. The
// dictionaries are embedded in the opencc package, so no files are needed.
var (
	convMu    sync.Mutex
	convCache = map[string]*opencc.OpenCC{}
	convFail  = map[string]bool{}
)

func convert(cfg, s string) (string, bool) {
	convMu.Lock()
	defer convMu.Unlock()
	if convFail[cfg] {
		return s, false
	}
	c := convCache[cfg]
	if c == nil {
		var err error
		c, err = opencc.New(cfg)
		if err != nil {
			convFail[cfg] = true
			return s, false
		}
		convCache[cfg] = c
	}
	out, err := c.Convert(s)
	if err != nil {
		return s, false
	}
	return out, true
}

func toSimplified(s string) (string, bool) { return convert("t2s", s) }

// isTraditional reports whether tag denotes Traditional Chinese.
func isTraditional(tag string) bool {
	t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	if baseLang(t) != "zh" {
		return false
	}
	for _, p := range strings.Split(t, "-")[1:] {
		switch p {
		case "tw", "hk", "mo", "hant":
			return true
		}
	}
	return false
}

// toTraditional converts s for a Traditional-Chinese book: Taiwan phrases for
// zh-TW, plain character conversion otherwise.
func toTraditional(s, tag string) (string, bool) {
	cfg := "s2t"
	if strings.Contains(strings.ToLower(tag), "tw") {
		cfg = "s2twp"
	}
	return convert(cfg, s)
}

// baseLang returns the lower-case primary subtag of a language tag.
func baseLang(tag string) string {
	t := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(tag, "_", "-")))
	if i := strings.IndexByte(t, '-'); i >= 0 {
		t = t[:i]
	}
	return t
}
