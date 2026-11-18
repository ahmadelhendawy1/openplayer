package main

// Interface language. English is built in; Arabic comes from lang_ar.json,
// keyed by the English text. The same table is sent to the HTML panels,
// so one file translates the whole player.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed lang_ar.json
var arJSON []byte

var (
	arText map[string]string
	uiLang = "en"
)

func init() {
	if err := json.Unmarshal(arJSON, &arText); err != nil {
		panic("lang_ar.json: " + err.Error())
	}
}

func rtl() bool { return uiLang == "ar" }

// T translates a piece of interface text.
func T(s string) string {
	if uiLang == "ar" {
		if v, ok := arText[s]; ok && v != "" {
			return v
		}
	}
	return s
}

// Tf translates a format string, then fills it in.
func Tf(format string, a ...any) string { return fmt.Sprintf(T(format), a...) }

// menuLabel translates "&Open File(s)...\tCtrl+O": the key is the text
// without "&" and without the shortcut; the shortcut is kept as is.
func menuLabel(s string) string {
	if uiLang != "ar" {
		return s
	}
	base, short, hasTab := strings.Cut(s, "\t")
	v, ok := arText[strings.ReplaceAll(base, "&", "")]
	if !ok || v == "" {
		return s
	}
	if hasTab {
		return v + "\t" + short
	}
	return v
}

// i18nJSON is what the HTML pages receive: language, direction, table.
func i18nJSON() string {
	m := map[string]any{"lang": uiLang, "dir": "ltr", "t": map[string]string{}}
	if uiLang == "ar" {
		m["dir"], m["t"] = "rtl", arText
	}
	b, _ := json.Marshal(m)
	return string(b)
}
