// Package lookup finds a canonical, same-language title for a book from
// reputable online catalogs.
package lookup

import (
	"context"

	"github.com/gkwang/epub-tools/internal/epub"
)

// Result of normalizing a title.
type Result struct {
	Title  string // final title to use (embedded title if no match)
	Source string // "bangumi", "google", "openlibrary", or "embedded"
	Warn   string // non-fatal problem (network error, 429), empty if none
}

// Normalizer resolves a book's canonical title. Implementations must never
// return an error for "no match" or network failure; they fall back to
// md.Title with Source "embedded" and set Warn.
type Normalizer interface {
	Normalize(ctx context.Context, md epub.Metadata) Result
}
