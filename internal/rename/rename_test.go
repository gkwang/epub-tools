package rename

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Plain Title", "Plain Title"},
		{"Title: Sub", "Title - Sub"},
		{`a/b\c*d?e"f<g>h|i`, "a-b-c_d_e_f_g_h_i"},
		{"tab\tand\nnewline  spaces", "tab and newline spaces"},
		{"ctl\x00\x07chars\x7f", "ctlchars"},
		{"  ..dots and spaces.. ", "dots and spaces"},
		{"...", ""},
		{"é", "é"}, // NFC
		{"「扉之魔术师」：第一卷！", "「扉之魔术师」：第一卷！"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTruncate(t *testing.T) {
	got := Sanitize(strings.Repeat("魔", 200))
	if len(got)+len(".epub") > 255 || !utf8.ValidString(got) {
		t.Fatalf("bad truncation: %d bytes valid=%v", len(got), utf8.ValidString(got))
	}
	if len(got) != 249 { // 83 runes * 3
		t.Errorf("len = %d, want 249", len(got))
	}
	if g := Sanitize(strings.Repeat("a", 300)); len(g) != 250 {
		t.Errorf("ascii len = %d", len(g))
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
		t.Fatal(err)
	}
}

// titles maps base file name -> title.
func resolver(titles map[string]string) Resolver {
	return func(p string) (string, string, error) {
		return titles[filepath.Base(p)], "", nil
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestPlanApplyBasicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.epub"))
	touch(t, filepath.Join(dir, "b.epub"))
	touch(t, filepath.Join(dir, "c.epub"))
	touch(t, filepath.Join(dir, "Existing.epub"))
	titles := map[string]string{"a.epub": "Same", "b.epub": "Same", "c.epub": "Existing", "Existing.epub": "Existing"}
	r := resolver(titles)
	run := func() []Action {
		files, err := Find(dir, false)
		if err != nil {
			t.Fatal(err)
		}
		acts := Apply(Plan(files, r))
		for _, a := range acts {
			if a.Err != nil {
				t.Fatalf("%s: %v", a.Old, a.Err)
			}
		}
		return acts
	}
	run()
	got := strings.Join(names(t, dir), ",")
	// Existing.epub keeps its name; c gets (2); a/b get Same and Same (2).
	want := "Existing (2).epub,Existing.epub,Same (2).epub,Same.epub"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	// Second run: everything unchanged. Titles now keyed by new names.
	titles = map[string]string{"Existing.epub": "Existing", "Existing (2).epub": "Existing", "Same.epub": "Same", "Same (2).epub": "Same"}
	r2 := resolver(titles)
	files, _ := Find(dir, false)
	for _, a := range Plan(files, r2) {
		if !a.Unchanged() {
			t.Errorf("not idempotent: %s -> %s (%v)", a.Old, a.New, a.Err)
		}
	}
}

func TestPlanNeverOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "x.epub"))
	touch(t, filepath.Join(dir, "Target.epub")) // unrelated file not in the file list
	acts := Plan([]string{filepath.Join(dir, "x.epub")}, resolver(map[string]string{"x.epub": "Target"}))
	if acts[0].New != "Target (2).epub" {
		t.Errorf("New = %q", acts[0].New)
	}
}

func TestEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "x.epub"))
	touch(t, filepath.Join(dir, "y.epub"))
	r := func(p string) (string, string, error) {
		if filepath.Base(p) == "x.epub" {
			return "", "", nil
		}
		return "...", "", nil
	}
	files, _ := Find(dir, false)
	acts := Apply(Plan(files, r))
	for _, a := range acts {
		if a.Err == nil || a.Err.Error() != "no usable title" || a.Old != a.New {
			t.Errorf("%+v", a)
		}
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("files changed: %v", got)
	}
}

func TestResolverError(t *testing.T) {
	boom := errors.New("boom")
	acts := Plan([]string{"/nonexistent/a.epub"}, func(string) (string, string, error) { return "", "", boom })
	if !errors.Is(acts[0].Err, boom) {
		t.Errorf("%+v", acts[0])
	}
}

func TestCaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "foo.epub"))
	files, _ := Find(dir, false)
	acts := Apply(Plan(files, resolver(map[string]string{"foo.epub": "Foo"})))
	if acts[0].Err != nil || acts[0].New != "Foo.epub" {
		t.Fatalf("%+v", acts[0])
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "Foo.epub" {
		t.Errorf("names = %v", got)
	}
}

func TestUppercaseExtension(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Book.EPUB"))
	files, _ := Find(dir, false)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	acts := Apply(Plan(files, resolver(map[string]string{"Book.EPUB": "Book"})))
	if acts[0].Err != nil || acts[0].New != "Book.epub" {
		t.Fatalf("%+v", acts[0])
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "Book.epub" {
		t.Errorf("names = %v", got)
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.epub"))
	touch(t, filepath.Join(dir, "b.EPUB"))
	touch(t, filepath.Join(dir, ".hidden.epub"))
	touch(t, filepath.Join(dir, "c.txt"))
	touch(t, filepath.Join(dir, "sub", "d.epub"))
	touch(t, filepath.Join(dir, ".git", "e.epub"))
	touch(t, filepath.Join(dir, "dir.epub", "f.epub"))
	rel := func(fs []string) string {
		var o []string
		for _, f := range fs {
			r, _ := filepath.Rel(dir, f)
			o = append(o, r)
		}
		return strings.Join(o, ",")
	}
	flat, err := Find(dir, false)
	if err != nil || rel(flat) != "a.epub,b.EPUB" {
		t.Errorf("flat = %v %v", rel(flat), err)
	}
	rec, err := Find(dir, true)
	if err != nil || rel(rec) != "a.epub,b.EPUB,dir.epub/f.epub,sub/d.epub" {
		t.Errorf("rec = %v %v", rel(rec), err)
	}
}

func TestPerDirectoryTargets(t *testing.T) {
	dir := t.TempDir()
	p1, p2 := filepath.Join(dir, "s1", "a.epub"), filepath.Join(dir, "s2", "a.epub")
	touch(t, p1)
	touch(t, p2)
	acts := Plan([]string{p1, p2}, resolver(map[string]string{"a.epub": "T"}))
	if acts[0].New != "T.epub" || acts[1].New != "T.epub" {
		t.Errorf("%+v", acts)
	}
}

func TestLongTitleWithSuffix(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("魔", 200)
	touch(t, filepath.Join(dir, "a.epub"))
	touch(t, filepath.Join(dir, "b.epub"))
	r := resolver(map[string]string{"a.epub": long, "b.epub": long})
	files, _ := Find(dir, false)
	acts := Apply(Plan(files, r))
	for _, a := range acts {
		if a.Err != nil || len(a.New) > 255 {
			t.Fatalf("%s len %d err %v", a.New, len(a.New), a.Err)
		}
	}
	if acts[0].New == acts[1].New {
		t.Error("same target")
	}
	// idempotent
	titles := map[string]string{acts[0].New: long, acts[1].New: long}
	files, _ = Find(dir, false)
	for _, a := range Plan(files, resolver(titles)) {
		if !a.Unchanged() {
			t.Errorf("not idempotent: %s -> %s", a.Old, a.New)
		}
	}
}

func TestApplyRechecksTarget(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.epub"))
	acts := Plan([]string{filepath.Join(dir, "a.epub")}, resolver(map[string]string{"a.epub": "T"}))
	touch(t, filepath.Join(dir, "T.epub")) // appears after planning
	out := Apply(acts)
	if out[0].Err == nil {
		t.Error("expected error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "T.epub")); !strings.HasSuffix(string(b), "T.epub") {
		t.Error("target overwritten")
	}
}
