//go:build windows

package main

import "strings"

var pGetUserDefaultUILang = kernel32.NewProc("GetUserDefaultUILanguage")

// initLanguage picks the saved language, or Arabic when Windows itself is in
// Arabic, otherwise English.
func initLanguage() {
	l := cfg.get("lang", "")
	if l != "en" && l != "ar" {
		l = "en"
		if r, _, _ := pGetUserDefaultUILang.Call(); r&0x3FF == 0x01 { // LANG_ARABIC
			l = "ar"
		}
	}
	uiLang = l
}

// uoscLanguages is the uosc "languages" option (read when uosc starts).
func uoscLanguages() string {
	if uiLang == "ar" {
		return "~~/uosc-ar.json,~~/openplayer-text.json,en"
	}
	return "~~/openplayer-text.json,en"
}

// uoscStartOpts is the script-opts value set before mpv starts ([...] quotes
// values that contain commas).
func uoscStartOpts() string {
	return "uosc-languages=[" + uoscLanguages() + "],uosc-controls=[" + uoscControls() + "],uosc-scale=" + cfg.get("ui_scale", "1")
}

// uoscControls is the control bar layout with translated tooltips.
func uoscControls() string {
	items := []string{
		"command:menu:script-message openplayer-panel menu?" + T("Menu"),
		"gap",
		"command:volume_up:script-message openplayer-volume?" + T("Volume (click again to mute)"),
		"command:graphic_eq:script-message openplayer-eq?" + T("Equalizer"),
		"gap",
		"<has_sub>command:subtitles:script-message openplayer-panel menu subtitles?" + T("Subtitles"),
		"<has_many_audio>command:audiotrack:script-message openplayer-panel menu audio?" + T("Audio track"),
		"space",
		"command:skip_previous:playlist-prev?" + T("Previous"),
		"command:replay_10:seek -10?" + T("Back 10 seconds"),
		"cycle:pause:pause:no=pause!/yes=play_arrow!?" + T("Play / Pause"),
		"command:forward_10:seek 10?" + T("Forward 10 seconds"),
		"command:skip_next:playlist-next?" + T("Next"),
		"space",
		"<video,audio>speed:1",
		"command:playlist_play:script-message openplayer-panel playlist?" + T("Playlist"),
		"fullscreen",
	}
	// uosc splits the list on commas: keep translated text free of them.
	for i, s := range items {
		items[i] = strings.ReplaceAll(s, ",", "،")
	}
	return strings.Join(items, ",")
}

func applyUoscText() {
	mpv.Async(mp, "change-list", "script-opts", "append", "uosc-controls="+uoscControls())
}

// setLanguage switches the whole interface at run time.
func setLanguage(l string) {
	if l == uiLang {
		return
	}
	uiLang = l
	_ = cfg.set("lang", l)
	rebuildMenus()
	applyUoscText()
	for _, p := range []*panel{leftPanel, rightPanel, idleView} {
		if p.ready {
			p.eval("OP.i18n(" + i18nJSON() + ")")
			p.pushAll()
		}
	}
	popupsSetLang()
	pushOptions()
	applyTitle()
	msg := map[string]string{"en": "Language: English", "ar": "اللغة: العربية"}[l]
	mpv.Async(mp, "show-text", msg, "2000")
}
