package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// interfaceStrings collects every piece of English interface text from the
// sources: menu labels, T()/Tf() calls, HTML data-t attributes and T() in
// page scripts, the Options list, presets and theme names.
func interfaceStrings(t *testing.T) []string {
	set := map[string]bool{}
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			set[s] = true
		}
	}
	addMenu := func(s string) {
		s = strings.ReplaceAll(s, "&", "")
		if i := strings.Index(s, "\t"); i >= 0 {
			s = s[:i]
		}
		add(s)
	}
	menuRe := []*regexp.Regexp{
		regexp.MustCompile(`\baddItem\(\w+, [^,]+, ("(?:[^"\\]|\\.)*")`),
		regexp.MustCompile(`\baddSub\(\w+, [^,]+, ("(?:[^"\\]|\\.)*")`),
		regexp.MustCompile(`\baddMpv\(\w+, ("(?:[^"\\]|\\.)*")`),
	}
	goRe := []*regexp.Regexp{
		regexp.MustCompile(`\bT[f]?\(("(?:[^"\\]|\\.)*")`),
	}
	htmlRe := []*regexp.Regexp{
		regexp.MustCompile(`data-t[ip]?="([^"]+)"`),
		regexp.MustCompile(`\bT\("([^"]+)"\)`),
		regexp.MustCompile(`\bT\(([a-z.]+ [=!]== [^?]+ \? "([^"]+)" : "([^"]+)")\)`),
	}
	files, _ := filepath.Glob("*.go")
	html, _ := filepath.Glob("*.html")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, re := range menuRe {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if s, err := strconv.Unquote(m[1]); err == nil {
					addMenu(s)
				}
			}
		}
		for _, re := range goRe {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if s, err := strconv.Unquote(m[1]); err == nil {
					add(s)
				}
			}
		}
	}
	tern := regexp.MustCompile(`T\([^()]*\? "([^"]+)" : "([^"]+)"\)`)
	for _, f := range html {
		b, _ := os.ReadFile(f)
		for _, re := range htmlRe[:2] {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				add(m[1])
			}
		}
		for _, m := range tern.FindAllStringSubmatch(string(b), -1) {
			add(m[1])
			add(m[2])
		}
	}
	for _, s := range optionSections() {
		add(s.Label)
		for _, o := range s.Items {
			add(o.Label)
			if o.Help != "" {
				add(o.Help)
			}
			if o.Kind == "action" {
				add(o.Def)
			}
			for _, c := range o.Choices {
				if _, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(c.L, "%"), "×"), 64); err != nil && c.L != c.V {
					add(c.L)
				}
			}
		}
	}
	for _, p := range eqPresets {
		add(p.Name)
	}
	for _, x := range extraStrings {
		add(x)
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// extraStrings are used through variables, not literals.
var extraStrings = []string{
	"Equalizer on", "Equalizer off", "Normalize on", "Normalize off", "item", "items", "Paused", "Playing",
	"Custom", "Presets", "My presets", "Restart needed", "Saved", "Saved. Restart OpenPlayer to apply.",
	"Automatic (Windows default)", "Default",
	"Aurora Glass", "Black Gold Aurora", "Aurora Night", "Honey Glass", "Burgundy Glass", "Retro White",
	"Midnight Glass", "Aurora", "Frost", "OLED Black", "High Contrast",
	"Menu", "Volume (click again to mute)", "Equalizer", "Subtitles", "Audio track", "Previous", "Back 10 seconds",
	"Play / Pause", "Forward 10 seconds", "Next", "Playlist",
}

func TestArabicComplete(t *testing.T) {
	var missing []string
	for _, s := range interfaceStrings(t) {
		if v, ok := arText[s]; !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		if os.Getenv("DUMP_MISSING") != "" {
			_ = os.WriteFile(os.Getenv("DUMP_MISSING"), []byte(strings.Join(missing, "\n")), 0o644)
		}
		t.Fatalf("%d interface strings have no Arabic translation, e.g. %q", len(missing), missing[0])
	}
	// format verbs must survive translation
	verb := regexp.MustCompile(`%[sdv%]`)
	for k, v := range arText {
		if strings.Join(verb.FindAllString(k, -1), "") != strings.Join(verb.FindAllString(v, -1), "") {
			t.Errorf("format verbs differ: %q -> %q", k, v)
		}
	}
}

func TestMenuLabel(t *testing.T) {
	defer func() { uiLang = "en" }()
	arText["Open File(s)..."] = "فتح ملفات..."
	uiLang = "ar"
	if got := menuLabel("&Open File(s)...\tCtrl+O"); got != "فتح ملفات...\tCtrl+O" {
		t.Fatalf("got %q", got)
	}
	uiLang = "en"
	if got := menuLabel("&Open File(s)...\tCtrl+O"); got != "&Open File(s)...\tCtrl+O" {
		t.Fatalf("got %q", got)
	}
}
