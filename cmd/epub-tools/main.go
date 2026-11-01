// Command epub-tools organizes an epub library.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gkwang/epub-tools/internal/epub"
	"github.com/gkwang/epub-tools/internal/lookup"
	"github.com/gkwang/epub-tools/internal/rename"
	"github.com/gkwang/epub-tools/internal/zh"
)

const usage = `epub-tools - organize an epub library

Usage:
  epub-tools rename [--apply] [--recursive] [--lookup] [--chinese traditional|simplified|keep] <dir>

Commands:
  rename    rename each epub to "<title>.epub" (preview unless --apply)
  version   print the version

Run "epub-tools rename -h" for flags.
`

func main() {
	v, _, _ := buildInfo()
	lookup.UserAgent = "epub-tools/" + v + " (https://github.com/gkwang/epub-tools)"
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "rename":
		return runRename(args[1:], stdout, stderr)
	case "version", "-v", "--version":
		printVersion(stdout)
		return 0
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runRename(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "perform the renames (default: preview only)")
	recursive := fs.Bool("recursive", false, "include subdirectories")
	useLookup := fs.Bool("lookup", false, "look up canonical titles online (network)")
	chinese := fs.String("chinese", string(zh.Traditional), "script for Chinese titles: traditional, simplified, or keep")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: epub-tools rename [--apply] [--recursive] [--lookup] [--chinese traditional|simplified|keep] <dir>")
		fs.PrintDefaults()
	}
	// Allow flags after the directory too.
	var pos []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(pos) != 1 {
		fs.Usage()
		return 2
	}
	dir := pos[0]
	script, ok := zh.ParseScript(*chinese)
	if !ok {
		fmt.Fprintf(stderr, "invalid --chinese %q: want traditional, simplified, or keep\n", *chinese)
		return 2
	}

	files, err := rename.Find(dir, *recursive)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var norm lookup.Normalizer
	if *useLookup {
		norm = lookup.New(lookup.Options{PreferTaiwan: script == zh.Traditional})
	}
	// Books whose title came from a catalog (not just a script change) get
	// the new title written into their metadata, keeping the old one.
	type metaUpdate struct{ title, old, lang string }
	metaUpdates := map[string]metaUpdate{}

	resolve := func(path string) (string, string, error) {
		md, err := epub.Read(path)
		if err != nil {
			return "", "", err
		}
		if md.Title == "" {
			return "", "", errors.New("no usable title")
		}
		if norm == nil {
			note := ""
			if md.TitleSource == epub.SourceNCX || md.TitleSource == epub.SourceNav {
				note = "title from " + string(md.TitleSource)
			}
			return zh.Normalize(md.Title, md.Lang, script), note, nil
		}
		res := norm.Normalize(context.Background(), md)
		if res.Warn != "" {
			fmt.Fprintf(stderr, "warning: %s: %s\n", path, res.Warn)
		}
		note := "matched: " + res.Source
		if res.Edition != "" {
			note += " (" + res.Edition + " edition)"
		}
		if res.Source == "" || res.Source == "embedded" {
			note = "embedded"
		}
		title := res.Title
		if title == "" {
			title = md.Title
		}
		title = zh.Normalize(title, md.Lang, script)
		if note != "embedded" && title != zh.Normalize(md.Title, md.Lang, script) {
			metaUpdates[filepath.Clean(path)] = metaUpdate{title, md.Title, md.Lang}
		}
		return title, note, nil
	}

	actions := rename.Plan(files, resolve)
	if *apply {
		actions = rename.Apply(actions)
	}

	var changed, unchanged, failed, metaChanged int
	for _, a := range actions {
		old := a.Path()
		switch {
		case a.Err != nil:
			failed++
			fmt.Fprintf(stdout, "error: %s: %v\n", old, a.Err)
			continue
		case a.Old == a.New:
			unchanged++
			fmt.Fprintf(stdout, "unchanged: %s\n", old)
		default:
			changed++
			line := fmt.Sprintf("%s -> %s", old, a.New)
			if a.Note != "" {
				line += " [" + a.Note + "]"
			}
			fmt.Fprintln(stdout, line)
		}

		mu, ok := metaUpdates[filepath.Clean(old)]
		if !ok {
			continue
		}
		if !*apply {
			metaChanged++
			fmt.Fprintf(stdout, "  would set title metadata to %q (keeping %q as alternate)\n", mu.title, mu.old)
			continue
		}
		did, err := epub.SetTitle(filepath.Join(a.Dir, a.New), mu.title, mu.old, mu.lang)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(stdout, "error: %s: title metadata not updated: %v\n", filepath.Join(a.Dir, a.New), err)
		case did:
			metaChanged++
			fmt.Fprintf(stdout, "  set title metadata to %q (kept %q as alternate)\n", mu.title, mu.old)
		}
	}
	verb := "would rename"
	if *apply {
		verb = "renamed"
	}
	fmt.Fprintf(stdout, "%s %d, unchanged %d, failed %d", verb, changed, unchanged, failed)
	if metaChanged > 0 {
		if *apply {
			fmt.Fprintf(stdout, ", metadata updated %d", metaChanged)
		} else {
			fmt.Fprintf(stdout, ", would update metadata %d", metaChanged)
		}
	}
	fmt.Fprintln(stdout)
	if !*apply && changed > 0 {
		fmt.Fprintln(stdout, "(preview only; use --apply to rename)")
	}
	if failed > 0 {
		return 1
	}
	return 0
}
