package epub

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	nsDC        = "http://purl.org/dc/elements/1.1/"
	nsOPF       = "http://www.idpf.org/2007/opf"
	generatedID = "epub-tools-title"
)

// opfEl is a direct child of <metadata> with its byte span in the OPF.
type opfEl struct {
	name        xml.Name
	raw         string // qualified name as written, e.g. "dc:title"
	attrs       []xml.Attr
	start       int // offset of '<'
	tagEnd      int // offset just past the start tag
	closeStart  int // offset of the end tag ('</'); == tagEnd when self-closing
	end         int // offset just past the element
	selfClosing bool
	text        string // direct character data
}

func (e *opfEl) attr(local string) string {
	for _, a := range e.attrs {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (e *opfEl) prefix() string {
	if i := strings.IndexByte(e.raw, ':'); i >= 0 {
		return e.raw[:i]
	}
	return ""
}

type opfDoc struct {
	data     []byte // BOM-stripped
	version  string
	mdEl     *opfEl // the <metadata> element itself
	declared map[string]bool
	children []*opfEl
	ids      map[string]bool
}

// parseOPF scans the package document, recording byte spans of the children
// of <metadata>. It also validates the whole document is well-formed UTF-8 XML.
func parseOPF(data []byte) (*opfDoc, error) {
	doc := &opfDoc{data: data, declared: map[string]bool{}, ids: map[string]bool{}}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = utf8Only
	depth := 0
	var cur *opfEl
	var md *opfEl
	for {
		off := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		end := int(d.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			for _, a := range t.Attr {
				if a.Name.Space == "" && a.Name.Local == "id" {
					doc.ids[a.Value] = true
				}
			}
			el := &opfEl{name: t.Name, attrs: t.Attr, start: off, tagEnd: end}
			el.raw = rawName(data[off:])
			el.selfClosing = bytes.HasSuffix(data[off:end], []byte("/>"))
			switch {
			case depth == 1:
				if t.Name.Local != "package" {
					return nil, fmt.Errorf("unexpected root element %q", t.Name.Local)
				}
				doc.version = strings.TrimSpace(el.attr("version"))
				doc.collectDecls(t)
			case depth == 2 && md == nil && t.Name.Local == "metadata":
				md = el
				doc.mdEl = el
				doc.collectDecls(t)
			case depth == 3 && md != nil && cur == nil:
				cur = el
			}
		case xml.CharData:
			if cur != nil && depth == 3 {
				cur.text += string(t)
			}
		case xml.EndElement:
			if depth == 3 && cur != nil {
				cur.end = end
				cur.closeStart = off
				if cur.selfClosing {
					cur.closeStart = cur.tagEnd
				}
				doc.children = append(doc.children, cur)
				cur = nil
			}
			depth--
		}
	}
	if doc.mdEl == nil {
		return nil, errors.New("no <metadata> element")
	}
	return doc, nil
}

func (doc *opfDoc) collectDecls(t xml.StartElement) {
	for _, a := range t.Attr {
		if a.Name.Space == "xmlns" {
			doc.declared[a.Name.Local] = true
		}
	}
}

func rawName(b []byte) string {
	if len(b) == 0 || b[0] != '<' {
		return ""
	}
	i := 1
	for i < len(b) && !strings.ContainsRune(" \t\r\n/>", rune(b[i])) {
		i++
	}
	return string(b[1:i])
}

func (doc *opfDoc) byLocal(local string) []*opfEl {
	var out []*opfEl
	for _, c := range doc.children {
		if c.name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// mainTitleEl mirrors Read's choice: title-type=main refined title, else the
// first usable title; as a last resort the first title even if junk.
func (doc *opfDoc) mainTitleEl() *opfEl {
	titles := doc.byLocal("title")
	metas := doc.byLocal("meta")
	for _, t := range titles {
		id := t.attr("id")
		if id == "" {
			continue
		}
		for _, m := range metas {
			if m.attr("property") == "title-type" && refID(m.attr("refines")) == id && strings.TrimSpace(m.text) == "main" {
				if !isJunk(normSpace(t.text)) {
					return t
				}
				goto fallback
			}
		}
	}
fallback:
	for _, t := range titles {
		if !isJunk(normSpace(t.text)) {
			return t
		}
	}
	if len(titles) > 0 {
		return titles[0]
	}
	return nil
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// qualified returns the tag name to use for a new element in namespace ns,
// copying prefix when it is usable in <metadata>'s scope, plus any xmlns
// declaration needed to make it valid.
func (doc *opfDoc) qualified(prefix, local, ns string) (name, decl string) {
	if prefix == "" {
		if doc.mdEl.name.Space == ns {
			return local, ""
		}
		return local, ` xmlns="` + ns + `"`
	}
	if doc.declared[prefix] {
		return prefix + ":" + local, ""
	}
	return local, ` xmlns="` + ns + `"`
}

func lineIndent(data []byte, pos int) (string, bool) {
	ls := bytes.LastIndexByte(data[:pos], '\n') + 1
	seg := data[ls:pos]
	if len(bytes.TrimLeft(seg, " \t")) != 0 {
		return "", false
	}
	return string(seg), true
}

func sepBefore(data []byte, pos int) string {
	if ind, ok := lineIndent(data, pos); ok {
		return "\n" + ind
	}
	return ""
}

var contentAttrRe = regexp.MustCompile(`(\scontent\s*=\s*)("[^"]*"|'[^']*')`)

type splice struct {
	start, end int
	text       string
}

// editOPF returns the new OPF bytes (BOM-stripped); changed=false means no
// edit is needed.
func editOPF(data []byte, newTitle, oldTitle, altLang string) ([]byte, bool, error) {
	doc, err := parseOPF(data)
	if err != nil {
		return nil, false, err
	}
	if doc.mdEl.selfClosing {
		return nil, false, errors.New("empty <metadata> element")
	}
	epub3 := strings.HasPrefix(doc.version, "3")
	main := doc.mainTitleEl()
	metas := doc.byLocal("meta")

	wantAlt := oldTitle != "" && normSpace(oldTitle) != normSpace(newTitle)
	if wantAlt && main != nil {
		if epub3 {
			if id := main.attr("id"); id != "" {
				for _, m := range metas {
					if m.attr("property") == "alternate-script" && refID(m.attr("refines")) == id && normSpace(m.text) == normSpace(oldTitle) {
						wantAlt = false
					}
				}
			}
		} else {
			for _, t := range doc.byLocal("title") {
				if t != main && normSpace(t.text) == normSpace(oldTitle) {
					wantAlt = false
				}
			}
		}
	}
	titleChanged := main == nil || main.text != newTitle
	if !titleChanged && !wantAlt {
		return data, false, nil
	}

	// id for refines (EPUB3).
	id := ""
	addID := false
	if main != nil {
		id = main.attr("id")
	}
	if epub3 && id == "" && (wantAlt || main == nil) {
		id = generatedID
		for n := 2; doc.ids[id]; n++ {
			id = fmt.Sprintf("%s-%d", generatedID, n)
		}
		addID = true
	}

	// Alternate-title element.
	alt := ""
	if wantAlt {
		if epub3 {
			prefix := ""
			for _, m := range metas {
				if m.attr("refines") != "" {
					prefix = m.prefix()
					break
				}
			}
			if prefix == "" && len(metas) > 0 && metas[0].prefix() != "" {
				prefix = metas[0].prefix()
			}
			name, decl := doc.qualified(prefix, "meta", nsOPF)
			lang := ""
			if altLang != "" {
				lang = ` xml:lang="` + esc(altLang) + `"`
			}
			alt = `<` + name + ` refines="#` + esc(id) + `" property="alternate-script"` + lang + decl + `>` + esc(oldTitle) + `</` + name + `>`
		} else {
			prefix := "dc"
			if main != nil {
				prefix = main.prefix()
			} else {
				for _, c := range doc.children {
					if c.name.Space == nsDC {
						prefix = c.prefix()
						break
					}
				}
			}
			name, decl := doc.qualified(prefix, "title", nsDC)
			alt = `<` + name + decl + `>` + esc(oldTitle) + `</` + name + `>`
		}
	}

	var sp []splice
	if main != nil {
		tag := string(data[main.start:main.tagEnd])
		if addID {
			n := 1 + len(main.raw)
			tag = tag[:n] + ` id="` + esc(id) + `"` + tag[n:]
		}
		var whole string
		if main.selfClosing {
			tag = strings.TrimRight(strings.TrimSuffix(tag, "/>"), " \t\r\n") + ">"
			whole = tag + esc(newTitle) + "</" + main.raw + ">"
		} else {
			whole = tag + esc(newTitle) + string(data[main.closeStart:main.end])
		}
		if alt != "" {
			whole += sepBefore(data, main.start) + alt
		}
		sp = append(sp, splice{main.start, main.end, whole})
	} else {
		prefix := "dc"
		for _, c := range doc.children {
			if c.name.Space == nsDC {
				prefix = c.prefix()
				break
			}
		}
		name, decl := doc.qualified(prefix, "title", nsDC)
		idAttr := ""
		if epub3 {
			idAttr = ` id="` + esc(id) + `"`
		}
		sep := ""
		if len(doc.children) > 0 {
			sep = sepBefore(data, doc.children[0].start)
		}
		ins := sep + `<` + name + idAttr + decl + `>` + esc(newTitle) + `</` + name + `>`
		if alt != "" {
			ins += sep + alt
		}
		sp = append(sp, splice{doc.mdEl.tagEnd, doc.mdEl.tagEnd, ins})
	}

	// Sort metadata stays consistent with the new title.
	if titleChanged {
		for _, m := range metas {
			switch {
			case m.attr("name") == "calibre:title_sort":
				tag := string(data[m.start:m.tagEnd])
				done := false
				nt := contentAttrRe.ReplaceAllStringFunc(tag, func(s string) string {
					if done {
						return s
					}
					done = true
					sub := contentAttrRe.FindStringSubmatch(s)
					return sub[1] + `"` + esc(newTitle) + `"`
				})
				if done && nt != tag {
					sp = append(sp, splice{m.start, m.tagEnd, nt})
				}
			case m.attr("property") == "calibre:title_sort" || (m.attr("property") == "file-as" && main != nil && main.attr("id") != "" && refID(m.attr("refines")) == main.attr("id")):
				if !m.selfClosing && m.text != newTitle {
					sp = append(sp, splice{m.tagEnd, m.closeStart, esc(newTitle)})
				}
			}
		}
	}

	sort.Slice(sp, func(i, j int) bool { return sp[i].start > sp[j].start })
	out := append([]byte(nil), data...)
	for _, s := range sp {
		out = append(out[:s.start], append([]byte(s.text), out[s.end:]...)...)
	}

	// Sanity: result must parse, with the new title and (if wanted) alt present.
	nd, err := parseOPF(out)
	if err != nil {
		return nil, false, fmt.Errorf("edited OPF is not well-formed: %w", err)
	}
	nm := nd.mainTitleEl()
	if nm == nil || nm.text != newTitle {
		return nil, false, errors.New("edited OPF does not resolve to the new main title")
	}
	if wantAlt {
		found := false
		if epub3 {
			for _, m := range nd.byLocal("meta") {
				if m.attr("property") == "alternate-script" && refID(m.attr("refines")) == nm.attr("id") && normSpace(m.text) == normSpace(oldTitle) && m.name.Space == nsOPF {
					found = true
				}
			}
		} else {
			for _, t := range nd.byLocal("title") {
				if t != nm && normSpace(t.text) == normSpace(oldTitle) && t.name.Space == nsDC {
					found = true
				}
			}
		}
		if !found {
			return nil, false, errors.New("edited OPF does not contain the alternate title")
		}
	}
	return out, true, nil
}

// stripZip64Extra removes the zip64 extended-info field so the writer can
// regenerate it.
func stripZip64Extra(extra []byte) []byte {
	var out []byte
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		n := int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+n > len(extra) {
			break
		}
		if id != 0x0001 {
			out = append(out, extra[:4+n]...)
		}
		extra = extra[4+n:]
	}
	return out
}

// SetTitle makes newTitle the book's main title and records oldTitle as an
// alternate title (altLang is its language tag, e.g. "zh", may be ""). It
// rewrites the file atomically. Returns changed=false if nothing needed doing.
func SetTitle(epubPath, newTitle, oldTitle, altLang string) (changed bool, err error) {
	if strings.TrimSpace(newTitle) == "" {
		return false, errors.New("empty new title")
	}
	if target, e := filepath.EvalSymlinks(epubPath); e == nil {
		epubPath = target
	}
	st, err := os.Stat(epubPath)
	if err != nil {
		return false, err
	}
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		return false, fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	opf := locateOPF(zr.File, files)
	if opf == nil {
		return false, errors.New("no OPF found in epub")
	}
	if enc := files["META-INF/encryption.xml"]; enc != nil {
		if b, e := readEntry(enc); e == nil && bytes.Contains(b, []byte(opf.Name)) {
			return false, errors.New("OPF is encrypted; refusing to modify")
		}
	}

	rc, err := opf.Open()
	if err != nil {
		return false, err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxXMLBytes+1))
	rc.Close()
	if err != nil {
		return false, err
	}
	if len(raw) > maxXMLBytes {
		return false, errors.New("OPF too large")
	}
	bom := []byte{}
	if bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		bom = raw[:3]
		raw = raw[3:]
	}
	edited, changed, err := editOPF(raw, newTitle, oldTitle, altLang)
	if err != nil {
		return false, fmt.Errorf("edit OPF %s: %w", opf.Name, err)
	}
	if !changed {
		return false, nil
	}
	edited = append(append([]byte(nil), bom...), edited...)

	dir := filepath.Dir(epubPath)
	tmp, err := os.CreateTemp(dir, ".epub-tools-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	zw := zip.NewWriter(tmp)
	if err := zw.SetComment(zr.Comment); err != nil {
		return false, err
	}
	for _, f := range zr.File {
		if f != opf {
			if err := zw.Copy(f); err != nil {
				return false, fmt.Errorf("copy %s: %w", f.Name, err)
			}
			continue
		}
		h := f.FileHeader
		if h.Method != zip.Store {
			h.Method = zip.Deflate
		}
		h.CRC32, h.CompressedSize, h.UncompressedSize = 0, 0, 0
		h.CompressedSize64, h.UncompressedSize64 = 0, 0
		h.Extra = stripZip64Extra(h.Extra)
		w, err := zw.CreateHeader(&h)
		if err != nil {
			return false, err
		}
		if _, err := w.Write(edited); err != nil {
			return false, err
		}
	}
	if err := zw.Close(); err != nil {
		return false, err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}

	md, err := Read(tmpName)
	if err != nil {
		return false, fmt.Errorf("verify rewritten epub: %w", err)
	}
	if md.Title != normSpace(newTitle) || md.TitleSource != SourceOPF {
		return false, fmt.Errorf("verify rewritten epub: title is %q (%s), want %q", md.Title, md.TitleSource, normSpace(newTitle))
	}
	if err := os.Rename(tmpName, epubPath); err != nil {
		return false, err
	}
	ok = true
	return true, nil
}
