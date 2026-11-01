// Package rename plans and applies <title>.epub renames.
package rename

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	ext        = ".epub"
	maxNameLen = 255 // bytes, common filesystem limit
	errNoTitle = "no usable title"
	tmpInfix   = ".epub-tools-tmp-"
)

// Resolver returns the title to use for the epub at path, plus an optional
// note to show the user.
type Resolver func(path string) (title string, note string, err error)

// Action is one planned (or applied) rename. Dir is the containing directory;
// Old and New are base names. New == Old means unchanged.
type Action struct {
	Dir, Old, New, Title, Note string
	Err                        error
}

// Unchanged reports whether the action is a no-op.
func (a Action) Unchanged() bool { return a.Err == nil && a.Old == a.New }

// Path returns the full source path.
func (a Action) Path() string { return filepath.Join(a.Dir, a.Old) }

// Sanitize turns a title into a safe base file name (no extension).
// Mapping: ':' -> " - "; '/' and '\' -> '-'; '*' '?' '"' '<' '>' '|' -> '_'.
// Control characters are dropped, all whitespace runs collapse to one space,
// leading/trailing dots and spaces are trimmed, and the result is cut at a
// rune boundary so that name+".epub" fits in 255 bytes. CJK punctuation is
// left untouched. The result may be empty.
func Sanitize(title string) string {
	return truncate(clean(title), maxNameLen-len(ext))
}

func clean(title string) string {
	title = norm.NFC.String(title)
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == ':':
			b.WriteString(" - ")
		case r == '/' || r == '\\':
			b.WriteByte('-')
		case strings.ContainsRune(`*?"<>|`, r):
			b.WriteByte('_')
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	return strings.Trim(s, ". ")
}

// truncate cuts s to at most max bytes on a rune boundary, then retrims.
func truncate(s string, max int) string {
	if len(s) > max {
		s = s[:max]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s = strings.Trim(s, ". ")
	}
	return s
}

// Find lists *.epub files (case-insensitive) in dir, sorted. Hidden files and
// directories, and directories named *.epub, are skipped.
func Find(dir string, recursive bool) ([]string, error) {
	var out []string
	if !recursive {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if isEpubFile(e) {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	} else {
		err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if p != dir && strings.HasPrefix(e.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if isEpubFile(e) {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func isEpubFile(e fs.DirEntry) bool {
	n := e.Name()
	if strings.HasPrefix(n, ".") || e.IsDir() {
		return false
	}
	return strings.EqualFold(filepath.Ext(n), ext)
}

// Plan computes actions for files (processed in sorted order). Nothing on
// disk is changed. Targets never collide with existing files or with each
// other within a directory.
func Plan(files []string, resolve Resolver) []Action {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	claimed := map[string]bool{} // dir + "\x00" + lower(name)
	key := func(dir, name string) string { return dir + "\x00" + strings.ToLower(norm.NFC.String(name)) }

	acts := make([]Action, 0, len(sorted))
	for _, f := range sorted {
		dir, old := filepath.Split(f)
		dir = filepath.Clean(dir)
		a := Action{Dir: dir, Old: old, New: old}
		title, note, err := resolve(f)
		a.Note = note
		if err == nil && clean(title) == "" {
			err = errors.New(errNoTitle)
		}
		if err != nil {
			a.Err = err
			acts = append(acts, a)
			continue
		}
		a.Title = title
		base := clean(title)
		a.New = pick(f, dir, old, base, func(n string) bool { return claimed[key(dir, n)] })
		claimed[key(dir, a.New)] = true
		acts = append(acts, a)
	}
	return acts
}

func pick(src, dir, old, base string, isClaimed func(string) bool) string {
	name := func(n int) string {
		suffix := ""
		if n > 1 {
			suffix = " (" + strconv.Itoa(n) + ")"
		}
		return truncate(base, maxNameLen-len(ext)-len(suffix)) + suffix + ext
	}
	// Idempotency: already named for its own title, with optional " (N)".
	if !isClaimed(old) && ownName(old, base) {
		return old
	}
	srcInfo, _ := os.Lstat(src)
	for n := 1; ; n++ {
		c := name(n)
		if isClaimed(c) {
			continue
		}
		if c == old {
			return old
		}
		if st, err := os.Lstat(filepath.Join(dir, c)); err == nil {
			if srcInfo != nil && os.SameFile(srcInfo, st) {
				return c // case-only (or normalization-only) rename
			}
			continue
		}
		return c
	}
}

// Apply performs the renames and returns the actions with Err set on failure.
// Unchanged and already-failed actions pass through.
func Apply(actions []Action) []Action {
	out := make([]Action, len(actions))
	copy(out, actions)
	for i := range out {
		a := &out[i]
		if a.Err != nil || a.Old == a.New {
			continue
		}
		a.Err = applyOne(*a)
	}
	return out
}

func applyOne(a Action) error {
	src, dst := filepath.Join(a.Dir, a.Old), filepath.Join(a.Dir, a.New)
	srcSt, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if dstSt, err := os.Lstat(dst); err == nil {
		if !os.SameFile(srcSt, dstSt) {
			return fmt.Errorf("target exists: %s", a.New)
		}
		// Same file under a different case: go through a temp name so it
		// works on case-insensitive filesystems.
		tmp := dst + tmpInfix + strconv.Itoa(os.Getpid())
		if err := os.Rename(src, tmp); err != nil {
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Rename(tmp, src)
			return err
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}

var suffixRe = regexp.MustCompile(` \((\d+)\)$`)

// ownName reports whether old is "<base>.epub" or "<base> (N).epub" (N >= 2).
func ownName(old, base string) bool {
	if filepath.Ext(old) != ext {
		return false
	}
	stem := strings.TrimSuffix(old, ext)
	if stem == truncate(base, maxNameLen-len(ext)) {
		return true
	}
	if m := suffixRe.FindStringSubmatchIndex(stem); m != nil {
		if n, err := strconv.Atoi(stem[m[2]:m[3]]); err == nil && n >= 2 {
			return stem[:m[0]] == truncate(base, maxNameLen-len(ext)-(len(stem)-m[0]))
		}
	}
	return false
}
