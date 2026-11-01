package lookup

import "strings"

// Taiwan ISBN registration-group prefixes (978-957, 978-986, 978-626).
var taiwanISBN13Prefixes = []string{"978957", "978986", "978626"}

// Group 957 and 986 as ISBN-10 prefixes.
var taiwanISBN10Prefixes = []string{"957", "986"}

// Substrings that mark a publisher as Taiwanese. Bare 角川 (Japanese
// KADOKAWA) is deliberately absent; only the Taiwan branch matches.
var taiwanPublishers = []string{
	"台灣", "臺灣", "台湾",
	"尖端", "東立", "青文", "台灣角川", "臺灣角川", "台灣東販", "臺灣東販",
	"長鴻", "威向", "獨步", "皇冠", "春天出版", "平心", "銘顯", "輕文學",
	"蓋亞", "青空", "未來數位",
}

func cleanISBN(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == 'X' || r == 'x' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isTaiwanISBN reports whether a (hyphenated or bare) ISBN has a Taiwan
// registration-group prefix.
func isTaiwanISBN(isbn string) bool {
	d := cleanISBN(isbn)
	prefixes := taiwanISBN13Prefixes
	switch len(d) {
	case 13:
	case 10:
		prefixes = taiwanISBN10Prefixes
	default:
		return false
	}
	for _, p := range prefixes {
		if strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

// IsTaiwanEdition reports whether a catalog entry looks like a Taiwan
// edition. ISBNs take precedence: if any usable ISBN is given, the entry is
// Taiwanese exactly when one of them has a Taiwan prefix (so a joint
// listing such as "芳文社、台灣東販" with a Japanese 9784 ISBN is not).
// Without an ISBN the publisher string decides.
func IsTaiwanEdition(publisher string, isbns ...string) bool {
	known := false
	for _, i := range isbns {
		if n := len(cleanISBN(i)); n == 10 || n == 13 {
			known = true
			if isTaiwanISBN(i) {
				return true
			}
		}
	}
	if known {
		return false
	}
	for _, p := range taiwanPublishers {
		if strings.Contains(publisher, p) {
			return true
		}
	}
	return false
}
