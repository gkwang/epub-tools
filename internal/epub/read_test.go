package epub

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGolden(t *testing.T) {
	door := []string{"空埜一樹", "ぽんじりつ"}
	noname := []string{"鴨野うどん", "かふか"}
	cases := []struct {
		file string
		want Metadata
	}{
		{"1(100).epub", Metadata{"扉之魔术师的召唤契约 第一卷 拥有最强之名的少女", SourceOPF, "zh", "zh", door, "", "扉之魔术师的召唤契约", "1"}},
		{"2(38).epub", Metadata{"扉之魔术师的召唤契约 第二卷 幻奏歌姬", SourceOPF, "zh", "zh", door, "", "扉之魔术师的召唤契约", "2"}},
		{"3(21).epub", Metadata{"扉之魔术师的召唤契约 第三卷 萨夫洛斯迷宫", SourceOPF, "zh", "zh", door, "", "扉之魔术师的召唤契约", "3"}},
		{"4(11).epub", Metadata{"扉之魔术师的召唤契约 第四卷 亚尔戈的遗产", SourceOPF, "zh", "zh", door, "", "扉之魔术师的召唤契约", "4"}},
		{"5(6).epub", Metadata{"扉之魔术师的召唤契约 第五卷 少年的壮烈之战", SourceOPF, "zh", "zh", door, "", "扉之魔术师的召唤契约", "5"}},
		{"1(60).epub", Metadata{"无法成为主角的我们始于妥协的恋人生活 第一卷", SourceOPF, "zh", "zh", noname, "", "无法成为主角的我们始于妥协的恋人生活", "1"}},
		{"2(25).epub", Metadata{"无法成为主角的我们始于妥协的恋人生活 第二卷", SourceOPF, "zh", "zh", noname, "", "无法成为主角的我们始于妥协的恋人生活", "2"}},
		{"1(67).epub", Metadata{"为了不让你忘记与我共度的夏天 第一卷", SourceOPF, "zh", "zh", []string{"國仲シンジ"}, "", "为了不让你忘记与我共度的夏天", "1"}},
		{"1(78).epub", Metadata{"前女友不幸转校归来，小暮理知的罠与恋", SourceOPF, "zh", "en", []string{"野村美月"}, "", "", ""}},
		{"1(81).epub", Metadata{"时日无多的你留给我的东西", SourceOPF, "zh", "zh", []string{"日野祐希"}, "", "", ""}},
		{"ngjm2e.epub", Metadata{"Whistler", SourceOPF, "en", "en", []string{"Patchett, Ann"}, "9780063511637", "", ""}},
		{"zht_来购买_用品的优等生 - 01.epub", Metadata{"來購買成人用品的優等生 第一卷 上", SourceOPF, "zh-TW", "zh-TW", []string{"4kaえんぴつ", "千種みのり"}, "", "來購買成人用品的優等生", "1"}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join("../../testdata", c.file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Skip("testdata epub not present (not committed)")
			}
			got, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

type files map[string]string

func build(t *testing.T, name string, f files) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for n, c := range f {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	out.Close()
	return p
}

const container = `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`

func opf(meta, manifest string) string {
	return `<?xml version="1.0" encoding="utf-8"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		meta + `</metadata><manifest>` + manifest + `</manifest></package>`
}

func ncx(title string) string {
	return `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><docTitle><text>` + title + `</text></docTitle></ncx>`
}

const ncxItem = `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`

func gen(t *testing.T, meta string, extra files) (Metadata, error) {
	f := files{"META-INF/container.xml": container, "OEBPS/content.opf": opf(meta, ncxItem)}
	for k, v := range extra {
		f[k] = v
	}
	return Read(build(t, "book.epub", f))
}

func TestNoTitleNCXFallback(t *testing.T) {
	m, err := gen(t, `<dc:language>en</dc:language>`, files{"OEBPS/toc.ncx": ncx("Book: Whistler")})
	if err != nil || m.Title != "Whistler" || m.TitleSource != SourceNCX {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestJunkTitle(t *testing.T) {
	m, _ := gen(t, `<dc:title>Unknown</dc:title>`, files{"OEBPS/toc.ncx": ncx("书名：真正的书名")})
	if m.Title != "真正的书名" || m.TitleSource != SourceNCX || m.Lang != "zh" {
		t.Fatalf("%+v", m)
	}
}

func TestNCXWithSeriesIndex(t *testing.T) {
	meta := `<dc:title>未知</dc:title><dc:language>zh</dc:language>
<meta property="belongs-to-collection" id="c1">某系列</meta>
<meta refines="#c1" property="collection-type">series</meta>
<meta refines="#c1" property="group-position">3</meta>`
	m, _ := gen(t, meta, files{"OEBPS/toc.ncx": ncx("某系列的故事")})
	if m.Title != "某系列的故事 第3卷" || m.Series != "某系列" || m.Index != "3" {
		t.Fatalf("%+v", m)
	}
	// already has a volume marker: unchanged
	m, _ = gen(t, meta, files{"OEBPS/toc.ncx": ncx("某系列的故事 第二卷")})
	if m.Title != "某系列的故事 第二卷" {
		t.Fatalf("%+v", m)
	}
	// non-CJK
	en := `<dc:language>en</dc:language><meta name="calibre:series" content="Saga"/><meta name="calibre:series_index" content="3.0"/>`
	m, _ = gen(t, en, files{"OEBPS/toc.ncx": ncx("Saga Book")})
	if m.Title != "Saga Book Vol. 3" || m.Series != "Saga" || m.Index != "3" {
		t.Fatalf("%+v", m)
	}
}

func TestNavTitleFallback(t *testing.T) {
	f := files{"META-INF/container.xml": container,
		"OEBPS/content.opf": opf(``, `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`),
		"OEBPS/nav.xhtml":   `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Navigation</title></head></html>`}
	m, err := Read(build(t, "a.epub", f))
	if err != nil || m.Title != "" {
		t.Fatalf("%+v %v", m, err)
	}
	f["OEBPS/nav.xhtml"] = `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Real Nav Title</title></head></html>`
	m, _ = Read(build(t, "a.epub", f))
	if m.Title != "Real Nav Title" || m.TitleSource != SourceNav {
		t.Fatalf("%+v", m)
	}
}

func TestNoLanguage(t *testing.T) {
	m, _ := gen(t, `<dc:title>Hello</dc:title>`, nil)
	if m.Lang != "" || m.DeclaredLang != "" {
		t.Fatalf("%+v", m)
	}
	m, _ = gen(t, `<dc:title>こんにちは</dc:title>`, nil)
	if m.Lang != "ja" {
		t.Fatalf("%+v", m)
	}
	m, _ = gen(t, `<dc:title>한국어</dc:title><dc:language>en</dc:language>`, nil)
	if m.Lang != "ko" {
		t.Fatalf("%+v", m)
	}
}

func TestMissingContainer(t *testing.T) {
	p := build(t, "x.epub", files{"book/pkg.opf": opf(`<dc:title>Fallback</dc:title>`, "")})
	m, err := Read(p)
	if err != nil || m.Title != "Fallback" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := Read(build(t, "y.epub", files{"a.txt": "x"})); err == nil {
		t.Fatal("expected error with no opf")
	}
}

func TestCorruptZip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.epub")
	os.WriteFile(p, []byte("not a zip"), 0o644)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing.epub")); err == nil {
		t.Fatal("expected error")
	}
}

func TestNoTitleAnywhere(t *testing.T) {
	m, err := gen(t, `<dc:creator>A B</dc:creator>`, nil)
	if err != nil || m.Title != "" || m.TitleSource != SourceNone || len(m.Authors) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestNonUTF8Encoding(t *testing.T) {
	o := `<?xml version="1.0" encoding="ISO-8859-1"?><package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>x</dc:title></metadata></package>`
	if _, err := Read(build(t, "e.epub", files{"META-INF/container.xml": container, "OEBPS/content.opf": o})); err == nil {
		t.Fatal("expected error")
	}
}

func TestISBN(t *testing.T) {
	cases := map[string]string{
		`<dc:identifier>urn:isbn:0-8044-2957-X</dc:identifier>`:                        "080442957X",
		`<dc:identifier>ISBN 978-0-06-351163-7</dc:identifier>`:                        "", // "ISBN " without colon is not a stripped prefix
		`<dc:identifier>isbn:978-0-06-351163-7</dc:identifier>`:                        "9780063511637",
		`<dc:identifier>9780063511638</dc:identifier>`:                                 "", // bad checksum
		`<dc:identifier>urn:uuid:12345678-1234-1234-1234-123456789012</dc:identifier>`: "",
		`<dc:identifier>1234567890123</dc:identifier>`:                                 "",
	}
	for in, want := range cases {
		m, _ := gen(t, in, nil)
		if m.ISBN != want {
			t.Errorf("%s: got %q want %q", in, m.ISBN, want)
		}
	}
	m, _ := gen(t, `<dc:identifier>uuid:12345678-1234-1234-1234-123456789012</dc:identifier><dc:identifier>9780063511637</dc:identifier>`, nil)
	if m.ISBN != "9780063511637" {
		t.Fatal(m.ISBN)
	}
}

func TestFilenameEqualsTitle(t *testing.T) {
	f := files{"META-INF/container.xml": container, "OEBPS/content.opf": opf(`<dc:title>Whistler</dc:title>`, "")}
	m, err := Read(build(t, "Whistler.epub", f))
	if err != nil || m.Title != "Whistler" || m.TitleSource != SourceOPF {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestAuthorRoles(t *testing.T) {
	m, _ := gen(t, `<dc:creator id="a">Smith,Jo</dc:creator><dc:creator id="b">Trans  Lator</dc:creator>
<meta refines="#a" property="role">aut</meta><meta refines="#b" property="role">trl</meta>`, nil)
	if !reflect.DeepEqual(m.Authors, []string{"Smith, Jo"}) {
		t.Fatalf("%v", m.Authors)
	}
	m, _ = gen(t, `<dc:creator>A</dc:creator><dc:creator>B</dc:creator>`, nil)
	if len(m.Authors) != 2 {
		t.Fatalf("%v", m.Authors)
	}
}

func TestMainTitleType(t *testing.T) {
	m, _ := gen(t, `<dc:title id="s">Sub</dc:title><dc:title id="m">Main</dc:title><meta refines="#m" property="title-type">main</meta>`, nil)
	if m.Title != "Main" {
		t.Fatalf("%+v", m)
	}
}
