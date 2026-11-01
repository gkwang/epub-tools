package lookup

import (
	"strings"
	"unicode"
)

var smallWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "but": true, "or": true,
	"nor": true, "for": true, "so": true, "yet": true, "at": true, "by": true,
	"in": true, "of": true, "on": true, "to": true, "up": true, "as": true, "vs": true,
}

// uniformCase reports whether the letters of s are all upper or all lower.
func uniformCase(s string) bool {
	up, low := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			up++
		case unicode.IsLower(r):
			low++
		}
	}
	return (up > 0) != (low > 0)
}

func capFirst(w string) string {
	r := []rune(w)
	for i, c := range r {
		if unicode.IsLetter(c) {
			r[i] = unicode.ToUpper(c)
			break
		}
	}
	return string(r)
}

// fixCasing repairs titles a catalog stored entirely in upper or lower case.
func fixCasing(title, lang string) string {
	if !uniformCase(title) {
		return title
	}
	words := strings.Fields(strings.ToLower(title))
	if len(words) == 0 {
		return title
	}
	switch baseLang(lang) {
	case "en":
		afterColon := true
		for i, w := range words {
			if i == 0 || i == len(words)-1 || afterColon || !smallWords[w] {
				words[i] = capFirst(w)
			}
			afterColon = strings.HasSuffix(w, ":")
		}
		return strings.Join(words, " ")
	case "fr", "es", "it", "pt", "nl":
		words[0] = capFirst(words[0])
		return strings.Join(words, " ")
	}
	return title
}
