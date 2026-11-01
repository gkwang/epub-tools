// Package epub extracts metadata from EPUB files. Every field is optional:
// real-world files are frequently incomplete or wrong.
package epub

// TitleSource records where Metadata.Title came from.
type TitleSource string

const (
	SourceNone TitleSource = ""
	SourceOPF  TitleSource = "opf"
	SourceNCX  TitleSource = "ncx"
	SourceNav  TitleSource = "nav"
)

// Metadata is what could be recovered from an EPUB. Any field may be zero.
type Metadata struct {
	Title        string      // best usable title, already junk-filtered; "" if none
	TitleSource  TitleSource // where Title came from
	Lang         string      // effective BCP 47 tag (script-detected, may override DeclaredLang), e.g. "zh", "zh-TW", "ja", "en"
	DeclaredLang string      // raw dc:language value as found
	Authors      []string    // normalized "Last, First" spacing fixed; file-as ignored
	ISBN         string      // digits only (ISBN-10 or -13, checksum-valid), "" if none
	Series       string      // belongs-to-collection or calibre:series
	Index        string      // group-position or calibre:series_index, as written ("1", "2.5")
}
