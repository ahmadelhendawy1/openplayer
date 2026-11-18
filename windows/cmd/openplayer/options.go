package main

// Tools > Options: the list of settings. "mpv:<name>" entries are engine
// properties (saved as "mpv.<name>" and applied at start-up and live);
// "app:<name>" entries are OpenPlayer settings; "act:<name>" are buttons.

import "strconv"

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

type optChoice struct {
	V string `json:"v"`
	L string `json:"l"`
}

type optDef struct {
	Key     string      `json:"key"`
	Label   string      `json:"label"`
	Kind    string      `json:"kind"` // choice, toggle, number, text, color, action
	Choices []optChoice `json:"choices,omitempty"`
	Min     float64     `json:"min,omitempty"`
	Max     float64     `json:"max,omitempty"`
	Step    float64     `json:"step,omitempty"`
	Def     string      `json:"def"`
	Restart bool        `json:"restart,omitempty"`
	Help    string      `json:"help,omitempty"`
	Value   string      `json:"value"`
}

type optSection struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Icon  string   `json:"icon"`
	Items []optDef `json:"items"`
}

func ch(pairs ...string) []optChoice {
	out := make([]optChoice, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, optChoice{pairs[i], pairs[i+1]})
	}
	return out
}

var yesNo = ch("yes", "On", "no", "Off")

func optionSections() []optSection {
	return []optSection{
		{ID: "interface", Label: "Interface", Icon: "ui", Items: []optDef{
			{Key: "app:lang", Label: "Language", Kind: "choice", Choices: ch("en", "English", "ar", "العربية"), Def: "en"},
			{Key: "app:theme", Label: "Theme", Kind: "choice", Def: "aurora-glass"}, // choices filled in at run time
			{Key: "app:menu_bar", Label: "Show the menu bar", Kind: "toggle", Def: "yes"},
			{Key: "app:ui_scale", Label: "Size of the on-screen controls", Kind: "choice", Choices: ch("1", "Normal", "1.15", "Large", "1.3", "Larger", "1.5", "Largest"), Def: "1"},
			{Key: "app:hide_ms", Label: "Hide the controls in fullscreen after (ms)", Kind: "number", Min: 500, Max: 10000, Step: 250, Def: "1500"},
			{Key: "mpv:osd-level", Label: "On-screen messages", Kind: "choice", Choices: ch("1", "On", "0", "Off"), Def: "1"},
			{Key: "app:single_instance", Label: "Open files in the running player", Kind: "toggle", Def: "yes", Help: "Opening a file while OpenPlayer is running adds it to the same window."},
		}},
		{ID: "playback", Label: "Playback", Icon: "play", Items: []optDef{
			{Key: "mpv:save-position-on-quit", Label: "Resume where I left off", Kind: "toggle", Def: "yes"},
			{Key: "app:resume_rewind", Label: "When resuming, go back (seconds)", Kind: "number", Min: 0, Max: 60, Step: 1, Def: "5", Help: "Replays the last few seconds so you remember where you were. 0 = start exactly where you stopped."},
			{Key: "app:restore_session", Label: "Reopen the last video when OpenPlayer starts", Kind: "toggle", Def: "yes", Help: "It opens paused at the moment you stopped. Click the video or press Play to continue."},
			{Key: "mpv:autocreate-playlist", Label: "Add the other files of the folder to the playlist", Kind: "choice", Choices: ch("no", "Off", "filter", "Same type of media", "same", "All media"), Def: "no"},
			{Key: "mpv:keep-open", Label: "At the end of the playlist", Kind: "choice", Choices: ch("yes", "Stop and go back to the beginning", "no", "Go back to the start screen"), Def: "yes"},
			{Key: "mpv:loop-playlist", Label: "Repeat the playlist", Kind: "choice", Choices: ch("no", "Off", "inf", "On"), Def: "no"},
			{Key: "mpv:hr-seek", Label: "Precise seeking", Kind: "choice", Choices: ch("default", "Automatic", "yes", "Always", "no", "Off (faster)"), Def: "default"},
			{Key: "mpv:speed", Label: "Default speed", Kind: "choice", Choices: ch("0.75", "0.75×", "1", "1× (normal)", "1.25", "1.25×", "1.5", "1.5×"), Def: "1"},
			{Key: "mpv:image-display-duration", Label: "Show pictures for (seconds)", Kind: "number", Min: 1, Max: 60, Step: 1, Def: "1"},
		}},
		{ID: "video", Label: "Video", Icon: "video", Items: []optDef{
			{Key: "mpv:hwdec", Label: "Hardware decoding", Kind: "choice", Choices: ch("auto-safe", "Automatic (recommended)", "d3d11va", "Direct3D 11", "nvdec", "NVIDIA", "dxva2", "DXVA2 (older GPUs)", "no", "Off (CPU only)"), Def: "auto-safe"},
			{Key: "mpv:vo", Label: "Video renderer", Kind: "choice", Choices: ch("gpu-next,gpu", "GPU Next (recommended)", "gpu", "GPU (compatible)", "direct3d", "Direct3D 9 (old PCs)"), Def: "gpu-next,gpu", Restart: true},
			{Key: "mpv:gpu-api", Label: "Graphics API", Kind: "choice", Choices: ch("auto", "Automatic", "d3d11", "Direct3D 11", "vulkan", "Vulkan", "opengl", "OpenGL"), Def: "auto", Restart: true},
			{Key: "mpv:scale", Label: "Upscaling quality", Kind: "choice", Choices: ch("bilinear", "Fast", "lanczos", "Good", "ewa_lanczossharp", "Best (needs a strong GPU)"), Def: "lanczos"},
			{Key: "mpv:dscale", Label: "Downscaling quality", Kind: "choice", Choices: ch("bilinear", "Fast", "hermite", "Good", "mitchell", "Soft", "ewa_lanczos", "Best"), Def: "hermite"},
			{Key: "app:smooth", Label: "Smooth motion (frame interpolation)", Kind: "toggle", Def: "no", Help: "Removes judder on displays whose refresh rate does not match the video."},
			{Key: "mpv:deband", Label: "Remove colour banding", Kind: "toggle", Def: "no"},
			{Key: "mpv:deinterlace", Label: "Deinterlace", Kind: "choice", Choices: ch("no", "Off", "yes", "On"), Def: "no"},
			{Key: "mpv:tone-mapping", Label: "HDR tone mapping", Kind: "choice", Choices: ch("auto", "Automatic", "bt.2390", "BT.2390", "spline", "Spline", "reinhard", "Reinhard", "clip", "Clip"), Def: "auto"},
			{Key: "mpv:target-colorspace-hint", Label: "Send HDR to HDR displays", Kind: "toggle", Def: "no"},
			{Key: "mpv:dither-depth", Label: "Dithering", Kind: "choice", Choices: ch("auto", "Automatic", "no", "Off", "8", "8-bit", "10", "10-bit"), Def: "auto"},
			{Key: "mpv:video-sync", Label: "Video sync", Kind: "choice", Choices: ch("audio", "Sync to audio", "display-resample", "Sync to display", "display-vdrop", "Drop frames to display"), Def: "audio"},
		}},
		{ID: "audio", Label: "Audio", Icon: "audio", Items: []optDef{
			{Key: "act:eq", Label: "Equalizer, normalize and amplifier", Kind: "action", Def: "Open equalizer"},
			{Key: "mpv:audio-device", Label: "Output device", Kind: "choice", Def: "auto"}, // filled in at run time
			{Key: "mpv:audio-exclusive", Label: "Exclusive mode (bit-perfect)", Kind: "toggle", Def: "no", Restart: true},
			{Key: "mpv:audio-channels", Label: "Speakers", Kind: "choice", Choices: ch("auto-safe", "Automatic", "stereo", "Stereo (2.0)", "5.1", "5.1 surround", "7.1", "7.1 surround", "mono", "Mono"), Def: "auto-safe"},
			{Key: "mpv:audio-spdif", Label: "Pass-through to receiver (S/PDIF, HDMI)", Kind: "choice", Choices: ch("", "Off", "ac3,dts", "Dolby Digital, DTS", "ac3,dts,eac3,truehd,dts-hd", "All formats"), Def: ""},
			{Key: "mpv:volume-max", Label: "Volume slider maximum", Kind: "choice", Choices: ch("100", "100%", "130", "130%", "150", "150%", "200", "200%"), Def: "130"},
			{Key: "mpv:audio-pitch-correction", Label: "Keep the pitch when changing speed", Kind: "toggle", Def: "yes"},
			{Key: "mpv:alang", Label: "Preferred audio languages", Kind: "text", Def: "", Help: "Language codes in order, for example: ara,eng"},
			{Key: "mpv:audio-file-auto", Label: "Load external audio files", Kind: "choice", Choices: ch("no", "Off", "exact", "Same name", "fuzzy", "Similar name"), Def: "fuzzy"},
		}},
		{ID: "subtitles", Label: "Subtitles", Icon: "sub", Items: []optDef{
			{Key: "mpv:sub-auto", Label: "Load subtitle files automatically", Kind: "choice", Choices: ch("no", "Off", "exact", "Same name", "fuzzy", "Similar name", "all", "All in the folder"), Def: "fuzzy"},
			{Key: "mpv:slang", Label: "Preferred subtitle languages", Kind: "text", Def: "", Help: "Language codes in order, for example: ara,eng"},
			{Key: "mpv:sub-codepage", Label: "Text encoding of old subtitle files", Kind: "choice", Choices: ch("auto", "Automatic", "cp1256", "Arabic (Windows-1256)", "iso-8859-6", "Arabic (ISO-8859-6)", "cp1252", "Western (Windows-1252)", "utf-8", "UTF-8"), Def: "auto", Help: "Choose Arabic if Arabic subtitles show strange characters."},
			{Key: "mpv:sub-font", Label: "Font", Kind: "choice", Choices: ch("Segoe UI", "Segoe UI", "Arial", "Arial", "Tahoma", "Tahoma", "Calibri", "Calibri", "Times New Roman", "Times New Roman", "Sakkal Majalla", "Sakkal Majalla", "Traditional Arabic", "Traditional Arabic"), Def: "Segoe UI"},
			{Key: "mpv:sub-font-size", Label: "Size", Kind: "number", Min: 20, Max: 90, Step: 1, Def: "38"},
			{Key: "mpv:sub-bold", Label: "Bold", Kind: "toggle", Def: "no"},
			{Key: "mpv:sub-color", Label: "Text colour", Kind: "color", Def: "#FFFFFF"},
			{Key: "mpv:sub-border-color", Label: "Outline colour", Kind: "color", Def: "#000000"},
			{Key: "mpv:sub-border-size", Label: "Outline thickness", Kind: "number", Min: 0, Max: 10, Step: 0.5, Def: "3"},
			{Key: "mpv:sub-shadow-offset", Label: "Shadow", Kind: "number", Min: 0, Max: 10, Step: 0.5, Def: "0"},
			{Key: "mpv:sub-pos", Label: "Vertical position (100 = bottom)", Kind: "number", Min: 0, Max: 150, Step: 1, Def: "100"},
			{Key: "mpv:sub-ass-override", Label: "Use my style for styled (ASS) subtitles", Kind: "choice", Choices: ch("no", "Off", "scale", "Size only", "force", "Always"), Def: "scale"},
		}},
		{ID: "files", Label: "Files & screenshots", Icon: "folder", Items: []optDef{
			{Key: "act:default", Label: "Default player", Kind: "action", Def: "Make OpenPlayer the default player…"},
			{Key: "mpv:screenshot-directory", Label: "Screenshot folder", Kind: "text", Def: "~~desktop/", Help: "~~desktop/ means your Desktop."},
			{Key: "mpv:screenshot-format", Label: "Screenshot format", Kind: "choice", Choices: ch("jpg", "JPEG", "png", "PNG (lossless)", "webp", "WebP"), Def: "jpg"},
			{Key: "mpv:screenshot-sw", Label: "Screenshots exactly as shown (with subtitles)", Kind: "toggle", Def: "no"},
		}},
		{ID: "performance", Label: "Performance & cache", Icon: "speed", Items: []optDef{
			{Key: "mpv:cache", Label: "Cache", Kind: "choice", Choices: ch("auto", "Automatic", "yes", "On", "no", "Off"), Def: "auto"},
			{Key: "mpv:demuxer-max-bytes", Label: "Read-ahead memory", Kind: "choice", Choices: ch("50MiB", "50 MB", "150MiB", "150 MB", "500MiB", "500 MB", "1GiB", "1 GB"), Def: "150MiB"},
			{Key: "mpv:demuxer-readahead-secs", Label: "Read ahead (seconds)", Kind: "number", Min: 1, Max: 600, Step: 1, Def: "1"},
			{Key: "mpv:vd-lavc-threads", Label: "Decoder threads (0 = automatic)", Kind: "number", Min: 0, Max: 32, Step: 1, Def: "0", Restart: true},
		}},
		{ID: "updates", Label: "Updates", Icon: "update", Items: []optDef{
			{Key: "app:auto_update", Label: "Update components automatically", Kind: "toggle", Def: "yes", Help: "The playback engine (FFmpeg codecs), controls and fonts. Every update is signed and can be rolled back."},
			{Key: "app:check_app", Label: "Tell me when a new OpenPlayer version is out", Kind: "toggle", Def: "yes", Help: "Checks ai4edutech.com when OpenPlayer starts. Nothing is installed without asking you."},
			{Key: "act:update", Label: "Updates", Kind: "action", Def: "Check for updates now"},
		}},
		{ID: "advanced", Label: "Advanced", Icon: "code", Items: []optDef{
			{Key: "act:mpvconf", Label: "Engine configuration file (mpv.conf)", Kind: "action", Def: "Edit mpv.conf", Help: "Any mpv option. Takes effect after restarting OpenPlayer."},
			{Key: "act:folder", Label: "Settings folder", Kind: "action", Def: "Open settings folder"},
			{Key: "act:reset", Label: "Reset", Kind: "action", Def: "Reset all options to default"},
		}},
	}
}

// findOpt returns the definition for key.
func findOpt(key string) (optDef, bool) {
	for _, s := range optionSections() {
		for _, o := range s.Items {
			if o.Key == key {
				return o, true
			}
		}
	}
	return optDef{}, false
}

// validOptValue checks a value coming from the Options page.
func validOptValue(o optDef, v string) bool {
	if len(v) > 300 {
		return false
	}
	switch o.Kind {
	case "toggle":
		return v == "yes" || v == "no"
	case "choice":
		if len(o.Choices) == 0 { // filled at run time (theme, device)
			return true
		}
		for _, c := range o.Choices {
			if c.V == v {
				return true
			}
		}
		return false
	case "number":
		f, err := parseFloat(v)
		return err == nil && f >= o.Min && f <= o.Max
	case "color":
		if len(v) != 7 || v[0] != '#' {
			return false
		}
		for _, c := range v[1:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
		return true
	case "text":
		for _, c := range v {
			if c < 32 {
				return false
			}
		}
		return true
	}
	return false
}
