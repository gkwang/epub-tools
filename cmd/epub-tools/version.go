package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// Set at release build time via -ldflags "-X main.version=... ".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// buildInfo returns the version, commit and date, falling back to the module
// and VCS information Go embeds in the binary (e.g. for `go install` builds).
func buildInfo() (v, c, d string) {
	v, c, d = version, commit, date
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

func printVersion(w io.Writer) {
	v, c, d := buildInfo()
	fmt.Fprintf(w, "epub-tools %s", v)
	if c != "" {
		if len(c) > 12 {
			c = c[:12]
		}
		fmt.Fprintf(w, " (commit %s", c)
		if d != "" {
			fmt.Fprintf(w, ", built %s", d)
		}
		fmt.Fprint(w, ")")
	}
	fmt.Fprintf(w, " %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
