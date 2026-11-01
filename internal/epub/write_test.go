package epub

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAll(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func opfText(t *testing.T, p string) string {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	return string(readAll(t, locateOPF(zr.File, files)))
}

func copyFile(t *testing.T, src string) string {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func checkMimetypeFirst(t *testing.T, p string) {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) == 0 || zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Errorf("mimetype not first/stored")
	}
}

func TestSetTitleTestdata(t *testing.T) {
	matches, _ := filepath.Glob("../../testdata/*.epub")
	if len(matches) == 0 {
		t.Skip("testdata epubs not present")
	}
	const newTitle = "新標題 第一卷"
	for _, src := range matches {
		t.Run(filepath.Base(src), func(t *testing.T) {
			orig, err := Read(src)
			if err != nil {
				t.Fatal(err)
			}
			p := copyFile(t, src)
			changed, err := SetTitle(p, newTitle, orig.Title, "zh")
			if err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			got, err := Read(p)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != newTitle || got.TitleSource != SourceOPF {
				t.Errorf("got %q (%s)", got.Title, got.TitleSource)
			}
			if got.ISBN != orig.ISBN || got.Series != orig.Series || got.Index != orig.Index || strings.Join(got.Authors, "|") != strings.Join(orig.Authors, "|") {
				t.Errorf("other metadata changed: %+v vs %+v", got, orig)
			}
			if zo, err := zip.OpenReader(src); err == nil {
				if len(zo.File) > 0 && zo.File[0].Name == "mimetype" {
					checkMimetypeFirst(t, p) // else: source has no leading mimetype; order is preserved as-is
				}
				zo.Close()
			}

			// Every non-OPF entry byte-identical, same order and names.
			zo, _ := zip.OpenReader(src)
			defer zo.Close()
			zn, _ := zip.OpenReader(p)
			defer zn.Close()
			if len(zo.File) != len(zn.File) {
				t.Fatalf("entry count %d vs %d", len(zo.File), len(zn.File))
			}
			files := map[string]*zip.File{}
			for _, f := range zo.File {
				files[f.Name] = f
			}
			opfName := locateOPF(zo.File, files).Name
			for i, f := range zo.File {
				n := zn.File[i]
				if n.Name != f.Name {
					t.Fatalf("order differs at %d: %s vs %s", i, n.Name, f.Name)
				}
				if f.Name == opfName {
					continue
				}
				if n.CRC32 != f.CRC32 || n.Method != f.Method || !bytes.Equal(readAll(t, n), readAll(t, f)) {
					t.Errorf("entry %s differs", f.Name)
				}
			}

			// Alt title present exactly once.
			text := opfText(t, p)
			if n := strings.Count(text, ">"+orig.Title+"</"); orig.Title != "" && n != 1+boolInt(strings.Contains(opfText(t, src), ">"+orig.Title+"</"))-1 {
				// original text may legitimately remain in other meta (file-as is updated); expect exactly one occurrence as alt.
				t.Errorf("old title occurs %d times in OPF", n)
			}
			doc, err := parseOPF([]byte(strings.TrimPrefix(text, "\xef\xbb\xbf")))
			if err != nil {
				t.Fatal(err)
			}
			main := doc.mainTitleEl()
			alts := 0
			if strings.HasPrefix(doc.version, "3") {
				for _, m := range doc.byLocal("meta") {
					if m.attr("property") == "alternate-script" && refID(m.attr("refines")) == main.attr("id") && m.text == orig.Title {
						alts++
					}
				}
				if main.attr("id") == "" {
					t.Error("no id on main title")
				}
			} else {
				for _, ti := range doc.byLocal("title") {
					if ti != main && ti.text == orig.Title {
						alts++
					}
				}
			}
			if alts != 1 {
				t.Errorf("alt titles = %d", alts)
			}
			if strings.Contains(text, "calibre:title_sort") && strings.Contains(opfText(t, src), "calibre:title_sort") {
				if !strings.Contains(text, `name="calibre:title_sort" content="`+newTitle+`"`) && !strings.Contains(text, `content="`+newTitle+`" name="calibre:title_sort"`) {
					t.Errorf("title_sort not updated")
				}
			}

			// Idempotent.
			before, _ := os.ReadFile(p)
			changed, err = SetTitle(p, newTitle, orig.Title, "zh")
			if err != nil || changed {
				t.Fatalf("second call changed=%v err=%v", changed, err)
			}
			after, _ := os.ReadFile(p)
			if !bytes.Equal(before, after) {
				t.Error("file modified by no-op call")
			}
		})
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestSetTitleKnownShapes(t *testing.T) {
	check := func(name, substr string) {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join("../../testdata", name)
			if _, err := os.Stat(src); err != nil {
				t.Skip("missing")
			}
			orig, _ := Read(src)
			p := copyFile(t, src)
			if _, err := SetTitle(p, "NEW", orig.Title, "zh"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(opfText(t, p), substr) {
				t.Errorf("OPF lacks %q", substr)
			}
		})
	}
	check("zht_来购买_用品的优等生 - 01.epub", `<opf:meta refines="#id" property="alternate-script" xml:lang="zh">`)
	check("1(100).epub", `<meta refines="#id" property="alternate-script" xml:lang="zh">`)
	check("1(78).epub", `name="calibre:title_sort" content="NEW"`)
	check("1(81).epub", `name="calibre:title_sort" content="NEW"`)
	check("ngjm2e.epub", `<meta refines="#en_title" property="alternate-script" xml:lang="zh">`)
}

const gcontainer = container

func genEpub(t *testing.T, opfXML string, extra files) string {
	f := files{"META-INF/container.xml": gcontainer, "OEBPS/content.opf": opfXML}
	for k, v := range extra {
		f[k] = v
	}
	return build(t, "g.epub", f)
}

func TestSetTitleEscaping(t *testing.T) {
	nt, ot := `A & B <"C"> d`, `Old & <x> "y"`
	for _, ver := range []string{"2.0", "3.0"} {
		meta := `<dc:title>` + esc(ot) + `</dc:title><dc:language>en</dc:language>`
		p := genEpub(t, strings.Replace(opf(meta, ""), `version="3.0"`, `version="`+ver+`"`, 1), nil)
		changed, err := SetTitle(p, nt, ot, `en"x`)
		if err != nil || !changed {
			t.Fatalf("%s: %v %v", ver, changed, err)
		}
		md, err := Read(p)
		if err != nil || md.Title != nt {
			t.Fatalf("%s: title %q err %v", ver, md.Title, err)
		}
		doc, err := parseOPF([]byte(opfText(t, p)))
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, c := range doc.children {
			if c.text == ot {
				found++
			}
		}
		if found != 1 {
			t.Errorf("%s: old title found %d times", ver, found)
		}
	}
}

func TestSetTitleNoDCTitle(t *testing.T) {
	for _, ver := range []string{"2.0", "3.0"} {
		o := strings.Replace(opf(`<dc:language>zh</dc:language>`, ncxItem), `version="3.0"`, `version="`+ver+`"`, 1)
		p := genEpub(t, o, files{"OEBPS/toc.ncx": ncx("旧名")})
		md, _ := Read(p)
		if md.TitleSource != SourceNCX {
			t.Fatalf("setup: %+v", md)
		}
		changed, err := SetTitle(p, "新名", md.Title, "zh")
		if err != nil || !changed {
			t.Fatal(changed, err)
		}
		md, err = Read(p)
		if err != nil || md.Title != "新名" || md.TitleSource != SourceOPF {
			t.Fatalf("%s: %+v %v", ver, md, err)
		}
		txt := opfText(t, p)
		if ver == "3.0" && !strings.Contains(txt, `<dc:title id="epub-tools-title">新名</dc:title><meta refines="#epub-tools-title" property="alternate-script" xml:lang="zh">旧名</meta>`) {
			t.Errorf("unexpected OPF: %s", txt)
		}
		if ver == "2.0" && !strings.Contains(txt, `<dc:title>新名</dc:title><dc:title>旧名</dc:title>`) {
			t.Errorf("unexpected OPF: %s", txt)
		}
		if c, err := SetTitle(p, "新名", "旧名", "zh"); err != nil || c {
			t.Errorf("second call: %v %v", c, err)
		}
	}
}

func TestSetTitleRefinedMainAndFileAs(t *testing.T) {
	meta := `<dc:title id="t0">Sub</dc:title><dc:title id="t1">Old</dc:title>` +
		`<meta refines="#t1" property="title-type">main</meta><meta refines="#t1" property="file-as">Old</meta>`
	p := genEpub(t, opf(meta, ""), nil)
	if _, err := SetTitle(p, "New", "Old", ""); err != nil {
		t.Fatal(err)
	}
	txt := opfText(t, p)
	for _, want := range []string{
		`<dc:title id="t1">New</dc:title><meta refines="#t1" property="alternate-script">Old</meta>`,
		`property="file-as">New</meta>`,
		`<dc:title id="t0">Sub</dc:title>`,
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in %s", want, txt)
		}
	}
}

func TestSetTitleCorruptZip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.epub")
	data := []byte("PK\x03\x04 this is not a real zip")
	os.WriteFile(p, data, 0o644)
	if _, err := SetTitle(p, "x", "y", ""); err == nil {
		t.Fatal("expected error")
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) {
		t.Error("file changed")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("leftover files: %v", ents)
	}
}

func TestSetTitleVerifyFailureLeavesNoTemp(t *testing.T) {
	// A junk new title is rejected by Read's filter -> verification fails.
	p := genEpub(t, opf(`<dc:title>Real</dc:title>`, ""), nil)
	before, _ := os.ReadFile(p)
	if _, err := SetTitle(p, "Unknown", "Real", ""); err == nil {
		t.Fatal("expected verification error")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Error("original modified")
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Errorf("leftover files: %v", ents)
	}
}

func TestSetTitlePermissionsAndComment(t *testing.T) {
	p := genEpub(t, opf(`<dc:title>Old</dc:title>`, ""), files{"mimetype": "application/epub+zip"})
	// Rewrite with a comment.
	b, _ := os.ReadFile(p)
	zr, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.SetComment("hello")
	for _, f := range zr.File {
		zw.Copy(f)
	}
	zw.Close()
	os.WriteFile(p, buf.Bytes(), 0o640)
	os.Chmod(p, 0o640)
	if _, err := SetTitle(p, "New", "Old", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	z, _ := zip.OpenReader(p)
	defer z.Close()
	if z.Comment != "hello" {
		t.Errorf("comment %q", z.Comment)
	}
}
