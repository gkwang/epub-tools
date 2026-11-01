package lookup

import "github.com/gkwang/epub-tools/internal/zh"

func toSimplified(s string) (string, bool)       { return zh.ToSimplified(s) }
func toTraditional(s, tag string) (string, bool) { return zh.ToTraditional(s, tag) }
func isTraditional(tag string) bool              { return zh.IsTraditional(tag) }
func baseLang(tag string) string                 { return zh.Base(tag) }
