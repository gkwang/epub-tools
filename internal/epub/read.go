package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const maxXMLBytes = 10 << 20

type xmlEl struct {
	ID    string `xml:"id,attr"`
	Role  string `xml:"role,attr"`
	Value string `xml:",chardata"`
}

type xmlMeta struct {
	ID       string `xml:"id,attr"`
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	Value    string `xml:",chardata"`
}

type xmlItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type xmlPackage struct {
	Titles      []xmlEl   `xml:"metadata>title"`
	Creators    []xmlEl   `xml:"metadata>creator"`
	Identifiers []xmlEl   `xml:"metadata>identifier"`
	Languages   []xmlEl   `xml:"metadata>language"`
	Metas       []xmlMeta `xml:"metadata>meta"`
	Items       []xmlItem `xml:"manifest>item"`
}

type xmlContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type xmlNCX struct {
	DocTitle string `xml:"docTitle>text"`
}

type xmlNav struct {
	Title string `xml:"head>title"`
}

func utf8Only(label string, input io.Reader) (io.Reader, error) {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "utf-8" || l == "utf8" {
		return input, nil
	}
	return nil, fmt.Errorf("unsupported XML encoding %q", label)
}

// readEntry reads a zip entry, capped at 10MB.
func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxXMLBytes))
	if err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), nil
}

func decodeXML(f *zip.File, v any, strict bool) error {
	b, err := readEntry(f)
	if err != nil {
		return err
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = strict
	if !strict {
		d.AutoClose = xml.HTMLAutoClose
		d.Entity = xml.HTMLEntity
	}
	d.CharsetReader = utf8Only
	return d.Decode(v)
}

// Read extracts metadata from the EPUB at path. It errors only if the file is
// not a readable zip or no OPF can be found/parsed; otherwise it returns
// whatever could be recovered (Title may be "").
func Read(epubPath string) (Metadata, error) {
	var md Metadata
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		return md, fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	var opf *zip.File
	if cf := files["META-INF/container.xml"]; cf != nil {
		var c xmlContainer
		if decodeXML(cf, &c, true) == nil {
			for _, rf := range c.Rootfiles {
				if f := files[strings.TrimPrefix(rf.FullPath, "/")]; f != nil && rf.FullPath != "" {
					opf = f
					break
				}
			}
		}
	}
	if opf == nil {
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".opf") && !f.FileInfo().IsDir() {
				opf = f
				break
			}
		}
	}
	if opf == nil {
		return md, errors.New("no OPF found in epub")
	}
	var pkg xmlPackage
	if err := decodeXML(opf, &pkg, true); err != nil {
		return md, fmt.Errorf("parse OPF %s: %w", opf.Name, err)
	}

	opfDir := path.Dir(opf.Name)

	if len(pkg.Languages) > 0 {
		md.DeclaredLang = strings.TrimSpace(pkg.Languages[0].Value)
	}
	md.Authors = authors(pkg)
	md.ISBN = findISBN(pkg)
	md.Series, md.Index = series(pkg)

	// Title cascade.
	try := func(t string, src TitleSource) bool {
		t = normSpace(t)
		if isJunk(t) {
			return false
		}
		md.Title, md.TitleSource = t, src
		return true
	}
	found := false
	if t := mainTitle(pkg); t != "" {
		found = try(t, SourceOPF)
	}
	if !found {
		for _, t := range pkg.Titles {
			if try(t.Value, SourceOPF) {
				found = true
				break
			}
		}
	}
	resolve := func(href string) *zip.File {
		if i := strings.IndexAny(href, "#?"); i >= 0 {
			href = href[:i]
		}
		if u, err := url.PathUnescape(href); err == nil {
			href = u
		}
		return files[path.Join(opfDir, href)]
	}
	if !found {
		var ncx *zip.File
		for _, it := range pkg.Items {
			if it.MediaType == "application/x-dtbncx+xml" {
				if ncx = resolve(it.Href); ncx != nil {
					break
				}
			}
		}
		if ncx == nil {
			for _, f := range zr.File {
				if strings.HasSuffix(strings.ToLower(f.Name), ".ncx") {
					ncx = f
					break
				}
			}
		}
		if ncx != nil {
			var n xmlNCX
			_ = decodeXML(ncx, &n, false)
			t := stripLabel(normSpace(n.DocTitle))
			if !isJunk(normSpace(t)) {
				lang := effectiveLang(t, md.DeclaredLang)
				t = addVolume(t, md.Index, lang)
				found = try(t, SourceNCX)
			}
		}
	}
	if !found {
		for _, it := range pkg.Items {
			if hasToken(it.Properties, "nav") {
				if f := resolve(it.Href); f != nil {
					var n xmlNav
					_ = decodeXML(f, &n, false)
					found = try(n.Title, SourceNav)
				}
				break
			}
		}
	}
	md.Lang = effectiveLang(md.Title, md.DeclaredLang)
	return md, nil
}

func hasToken(s, tok string) bool {
	for _, f := range strings.Fields(s) {
		if f == tok {
			return true
		}
	}
	return false
}

func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func refID(r string) string { return strings.TrimPrefix(strings.TrimSpace(r), "#") }

func mainTitle(p xmlPackage) string {
	for _, t := range p.Titles {
		if t.ID == "" {
			continue
		}
		for _, m := range p.Metas {
			if m.Property == "title-type" && refID(m.Refines) == t.ID && strings.TrimSpace(m.Value) == "main" {
				return t.Value
			}
		}
	}
	return ""
}

var uuidRe = regexp.MustCompile(`(?i)^(urn:)?(uuid:)?\{?[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}\}?$`)

var junkTitles = map[string]bool{
	"": true, "unknown": true, "untitled": true, "未知": true, "navigation": true,
	"contents": true, "table of contents": true, "目录": true, "目錄": true,
}

func isJunk(t string) bool {
	l := strings.ToLower(strings.TrimSpace(t))
	return junkTitles[l] || uuidRe.MatchString(l)
}

var labelRe = regexp.MustCompile(`(?i)^\s*(book|title|书名|書名|タイトル)\s*[:：]\s*`)

func stripLabel(s string) string { return labelRe.ReplaceAllString(s, "") }

const numRe = `[0-9０-９一二三四五六七八九十百零〇两兩]+`

var volRe = regexp.MustCompile(`第\s*` + numRe + `\s*[卷巻册冊集部]|卷\s*` + numRe + `|(?i:\bvol(ume)?\.?\s*\d+)`)

func addVolume(title, index, lang string) string {
	index = strings.TrimSpace(index)
	if title == "" || index == "" || volRe.MatchString(title) {
		return title
	}
	if strings.HasPrefix(lang, "zh") || strings.HasPrefix(lang, "ja") {
		return title + " 第" + index + "卷"
	}
	return title + " Vol. " + index
}

func effectiveLang(title, declared string) string {
	var kana, han, hangul bool
	for _, r := range title {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana = true
		case unicode.Is(unicode.Han, r):
			han = true
		case unicode.Is(unicode.Hangul, r):
			hangul = true
		}
	}
	d := strings.ToLower(declared)
	switch {
	case kana:
		return "ja"
	case han:
		if strings.HasPrefix(d, "ja") {
			return declared
		}
		if strings.HasPrefix(d, "zh") {
			return declared
		}
		return "zh"
	case hangul:
		return "ko"
	}
	return declared
}

var commaRe = regexp.MustCompile(`,(\S)`)

func authors(p xmlPackage) []string {
	roleOf := func(c xmlEl) (string, bool) {
		if strings.TrimSpace(c.Role) != "" {
			return strings.TrimSpace(c.Role), true
		}
		if c.ID != "" {
			for _, m := range p.Metas {
				if m.Property == "role" && refID(m.Refines) == c.ID {
					return strings.TrimSpace(m.Value), true
				}
			}
		}
		return "", false
	}
	anyRole := false
	for _, c := range p.Creators {
		if _, ok := roleOf(c); ok {
			anyRole = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range p.Creators {
		if anyRole {
			if r, _ := roleOf(c); r != "aut" {
				continue
			}
		}
		n := commaRe.ReplaceAllString(normSpace(c.Value), ", $1")
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func findISBN(p xmlPackage) string {
	for _, id := range p.Identifiers {
		s := strings.TrimSpace(id.Value)
		l := strings.ToLower(s)
		for _, pre := range []string{"urn:isbn:", "isbn:"} {
			if strings.HasPrefix(l, pre) {
				s = strings.TrimSpace(s[len(pre):])
			}
		}
		s = strings.NewReplacer("-", "", " ", "").Replace(s)
		if validISBN(s) {
			return strings.ToUpper(s)
		}
	}
	return ""
}

func validISBN(s string) bool {
	switch len(s) {
	case 13:
		if !strings.HasPrefix(s, "978") && !strings.HasPrefix(s, "979") {
			return false
		}
		sum := 0
		for i, r := range s {
			if r < '0' || r > '9' {
				return false
			}
			d := int(r - '0')
			if i%2 == 1 {
				d *= 3
			}
			sum += d
		}
		return sum%10 == 0
	case 10:
		sum := 0
		for i, r := range s {
			var d int
			switch {
			case r >= '0' && r <= '9':
				d = int(r - '0')
			case (r == 'X' || r == 'x') && i == 9:
				d = 10
			default:
				return false
			}
			sum += d * (10 - i)
		}
		return sum%11 == 0
	}
	return false
}

func series(p xmlPackage) (name, index string) {
	var coll *xmlMeta
	for i := range p.Metas {
		m := &p.Metas[i]
		if m.Property != "belongs-to-collection" || normSpace(m.Value) == "" {
			continue
		}
		if coll == nil {
			coll = m
		}
		for _, t := range p.Metas {
			if t.Property == "collection-type" && m.ID != "" && refID(t.Refines) == m.ID && strings.TrimSpace(t.Value) == "series" {
				coll = m
				goto picked
			}
		}
	}
picked:
	if coll != nil {
		name = normSpace(coll.Value)
		if coll.ID != "" {
			for _, m := range p.Metas {
				if m.Property == "group-position" && refID(m.Refines) == coll.ID {
					index = strings.TrimSpace(m.Value)
					break
				}
			}
		}
		return
	}
	for _, m := range p.Metas {
		switch m.Name {
		case "calibre:series":
			if name == "" {
				name = normSpace(m.Content)
			}
		case "calibre:series_index":
			if index == "" {
				index = strings.TrimSpace(m.Content)
			}
		}
	}
	if name == "" {
		index = ""
	} else if f, err := strconv.ParseFloat(index, 64); err == nil && f == float64(int(f)) {
		index = strconv.Itoa(int(f))
	}
	return
}
