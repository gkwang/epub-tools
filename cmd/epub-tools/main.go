// Command epub-tools organizes an epub library.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gkwang/epub-tools/internal/epub"
	"github.com/gkwang/epub-tools/internal/lookup"
	"github.com/gkwang/epub-tools/internal/rename"
)

const usage = `epub-tools - organize an epub library

Usage:
  epub-tools rename [--apply] [--recursive] [--lookup] <dir>

Commands:
  rename    rename each epub to "<title>.epub" (preview unless --apply)

Run "epub-tools rename -h" for flags.
`

func main() {
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
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: epub-tools rename [--apply] [--recursive] [--lookup] <dir>")
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

	files, err := rename.Find(dir, *recursive)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var norm lookup.Normalizer
	if *useLookup {
		norm = lookup.New()
	}
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
			return md.Title, note, nil
		}
		res := norm.Normalize(context.Background(), md)
		if res.Warn != "" {
			fmt.Fprintf(stderr, "warning: %s: %s\n", path, res.Warn)
		}
		note := "matched: " + res.Source
		if res.Source == "" || res.Source == "embedded" {
			note = "embedded"
		}
		title := res.Title
		if title == "" {
			title = md.Title
		}
		return title, note, nil
	}

	actions := rename.Plan(files, resolve)
	if *apply {
		actions = rename.Apply(actions)
	}

	var changed, unchanged, failed int
	for _, a := range actions {
		old := a.Path()
		switch {
		case a.Err != nil:
			failed++
			fmt.Fprintf(stdout, "error: %s: %v\n", old, a.Err)
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
	}
	verb := "would rename"
	if *apply {
		verb = "renamed"
	}
	fmt.Fprintf(stdout, "%s %d, unchanged %d, failed %d\n", verb, changed, unchanged, failed)
	if !*apply && changed > 0 {
		fmt.Fprintln(stdout, "(preview only; use --apply to rename)")
	}
	if failed > 0 {
		return 1
	}
	return 0
}
