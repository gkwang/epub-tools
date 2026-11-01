package zh

import "testing"

func TestToTraditional(t *testing.T) {
	cases := []struct{ in, tag, want string }{
		// Taiwan-standard characters (為, 裡, 著), not OpenCC's 爲/裏/着.
		{"为了不让你忘记与我共度的夏天 第一卷", "zh", "為了不讓你忘記與我共度的夏天 第一卷"},
		{"里面 着", "zh-CN", "裡面 著"},
		// No vocabulary substitution: 信息 stays 信息 (s2twp would give 資訊).
		{"信息 软件", "zh", "信息 軟件"},
		// Japanese kanji inside a Chinese title is left alone.
		{"小暮理知的罠与恋", "zh", "小暮理知的罠與戀"},
		// Hong Kong forms for HK/MO tags.
		{"里面 着", "zh-HK", "裏面 着"},
	}
	for _, c := range cases {
		got, ok := ToTraditional(c.in, c.tag)
		if !ok || got != c.want {
			t.Errorf("ToTraditional(%q,%q)=%q,%v want %q", c.in, c.tag, got, ok, c.want)
		}
	}
}

// Converting an already-Traditional title must not change it, or re-runs
// would rename files again.
func TestToTraditionalIdempotent(t *testing.T) {
	for _, s := range []string{
		"來購買成人用品的優等生 第一卷 上",
		"扉之魔術師的召喚契約 第一卷 擁有最強之名的少女",
		"前女友不幸轉校歸來，小暮理知的罠與戀",
		"Whistler",
	} {
		if got, _ := ToTraditional(s, "zh"); got != s {
			t.Errorf("ToTraditional(%q)=%q, want unchanged", s, got)
		}
	}
}

func TestToSimplified(t *testing.T) {
	if got, ok := ToSimplified("來購買成人用品的優等生"); !ok || got != "来购买成人用品的优等生" {
		t.Errorf("got %q,%v", got, ok)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		title, lang string
		target      Script
		want        string
	}{
		{"无法成为主角的我们 第一卷", "zh", Traditional, "無法成為主角的我們 第一卷"},
		{"來購買成人用品的優等生 第一卷 上", "zh-TW", Traditional, "來購買成人用品的優等生 第一卷 上"},
		{"來購買成人用品的優等生 第一卷 上", "zh-TW", Simplified, "来购买成人用品的优等生 第一卷 上"},
		{"无法成为主角的我们", "zh", Keep, "无法成为主角的我们"},
		// Japanese titles share kanji with Chinese but must never be converted.
		{"扉の魔術師の召喚契約", "ja", Traditional, "扉の魔術師の召喚契約"},
		{"小説", "ja", Simplified, "小説"},
		{"Whistler", "en", Traditional, "Whistler"},
		{"无法成为主角的我们", "", Traditional, "无法成为主角的我们"},
	}
	for _, c := range cases {
		if got := Normalize(c.title, c.lang, c.target); got != c.want {
			t.Errorf("Normalize(%q,%q,%s)=%q want %q", c.title, c.lang, c.target, got, c.want)
		}
	}
}

func TestParseScript(t *testing.T) {
	for in, want := range map[string]Script{"traditional": Traditional, " Simplified ": Simplified, "KEEP": Keep} {
		if got, ok := ParseScript(in); !ok || got != want {
			t.Errorf("ParseScript(%q)=%q,%v", in, got, ok)
		}
	}
	if _, ok := ParseScript("hant"); ok {
		t.Error("ParseScript(hant) should fail")
	}
}

func TestTags(t *testing.T) {
	for tag, want := range map[string]bool{"zh-TW": true, "zh_hk": true, "zh-Hant": true, "zh-Hant-TW": true, "zh": false, "zh-CN": false, "ja": false, "": false} {
		if got := IsTraditional(tag); got != want {
			t.Errorf("IsTraditional(%q)=%v want %v", tag, got, want)
		}
	}
	for tag, want := range map[string]bool{"zh": true, "ZH-tw": true, "ja": false, "": false} {
		if got := IsChinese(tag); got != want {
			t.Errorf("IsChinese(%q)=%v want %v", tag, got, want)
		}
	}
}
