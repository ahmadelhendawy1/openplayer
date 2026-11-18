//go:build windows

// OpenPlayer Setup: a glass-styled installer/uninstaller. The screens are an
// HTML page shown in Microsoft Edge WebView2; this program does the actual
// work (extract files, registry, shortcuts) and reports progress to the page.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"openplayer/internal/mediaext"
)

var version = "dev" // -ldflags "-X main.version=..."

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	shell32        = windows.NewLazySystemDLL("shell32.dll")
	ole32          = windows.NewLazySystemDLL("ole32.dll")
	dwmapi         = windows.NewLazySystemDLL("dwmapi.dll")
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	pMessageBoxW   = user32.NewProc("MessageBoxW")
	pFindWindowW   = user32.NewProc("FindWindowW")
	pPostMessageW  = user32.NewProc("PostMessageW")
	pShellExecuteW = shell32.NewProc("ShellExecuteW")
	pSHChange      = shell32.NewProc("SHChangeNotify")
	pBrowseFolder  = shell32.NewProc("SHBrowseForFolderW")
	pPathFromIDL   = shell32.NewProc("SHGetPathFromIDListW")
	pCoTaskMemFree = ole32.NewProc("CoTaskMemFree")
	pDwmSetAttr    = dwmapi.NewProc("DwmSetWindowAttribute")
	pRegDeleteTree = advapi32.NewProc("RegDeleteTreeW")
)

const (
	appKey       = `Software\OpenPlayer`
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer`
	progID       = "OpenPlayer.Media"
	webview2URL  = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
)

type options struct {
	Dir     string `json:"dir"`
	Theme   string `json:"theme"`
	Start   bool   `json:"start"`
	Desktop bool   `json:"desktop"`
	Context bool   `json:"context"`
	Assoc   bool   `json:"assoc"`
	Update  bool   `json:"update"`
}

type progress struct {
	Percent  float64 `json:"percent"`
	Status   string  `json:"status,omitempty"`
	DoneStep string  `json:"done_step,omitempty"`
	Error    string  `json:"error,omitempty"`
	Finished bool    `json:"finished,omitempty"`
}

var (
	wv        webview2.WebView
	hwnd      uintptr
	mode      = "install"
	installed options // remembered for the Finish page
	busy      sync.Mutex
)

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func msgBox(text string, flags uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("OpenPlayer Setup"))), flags)
	return r
}

func shellOpen(target string) {
	pShellExecuteW.Call(hwnd, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(target))), 0, 0, 1)
}

func report(p progress) {
	b, _ := json.Marshal(p)
	wv.Dispatch(func() { wv.Eval("window.onProgress(" + string(b) + ")") })
}

func defaultDir() string {
	if k, err := registry.OpenKey(registry.CURRENT_USER, appKey, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if d, _, err := k.GetStringValue("InstallDir"); err == nil && d != "" {
			return d
		}
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "OpenPlayer")
}

// ---- settings file (config\openplayer.conf), kept compatible with the player

func readConf(path string) ([]string, map[string]string) {
	var order []string
	m := map[string]string{}
	b, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			k = strings.TrimSpace(k)
			if _, seen := m[k]; !seen {
				order = append(order, k)
			}
			m[k] = strings.TrimSpace(v)
		}
	}
	return order, m
}

func writeConf(path string, set map[string]string) error {
	order, m := readConf(path)
	for k, v := range set {
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = v
	}
	var b strings.Builder
	b.WriteString("# OpenPlayer settings\r\n")
	for _, k := range order {
		b.WriteString(k + "=" + m[k] + "\r\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---- bindings called from the page -------------------------------------------

func opInfo() string {
	dir := defaultDir()
	if mode == "uninstall" {
		dir = installDirArg
	}
	theme := "aurora-glass" // fresh installs; an existing user's choice is kept below
	if _, m := readConf(filepath.Join(dir, "config", "openplayer.conf")); m["theme"] != "" {
		theme = m["theme"]
	}
	var need uint64
	if zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload))); err == nil {
		for _, f := range zr.File {
			need += f.UncompressedSize64
		}
	}
	var free uint64
	vol := filepath.VolumeName(dir) + `\`
	_ = windows.GetDiskFreeSpaceEx(u16(vol), &free, nil, nil)
	b, _ := json.Marshal(map[string]any{
		"mode": mode, "version": version, "dir": dir, "theme": theme,
		"requiredMB": float64(need) / (1 << 20), "freeMB": float64(free) / (1 << 20),
		"icon": "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG),
	})
	return string(b)
}

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	Param       uintptr
	Image       int32
}

func opBrowse(current string) string {
	name := make([]uint16, windows.MAX_PATH)
	bi := browseInfo{Owner: hwnd, DisplayName: &name[0], Title: u16("Choose where to install OpenPlayer"), Flags: 0x1 | 0x40}
	pidl, _, _ := pBrowseFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 1024)
	if r, _, _ := pPathFromIDL.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return ""
	}
	dir := windows.UTF16ToString(buf)
	if !strings.EqualFold(filepath.Base(dir), "OpenPlayer") {
		dir = filepath.Join(dir, "OpenPlayer")
	}
	return dir
}

func opInstall(raw string) {
	var o options
	if err := json.Unmarshal([]byte(raw), &o); err != nil || o.Dir == "" || !filepath.IsAbs(o.Dir) {
		report(progress{Error: "Please choose a valid installation folder."})
		return
	}
	go func() {
		busy.Lock()
		defer busy.Unlock()
		if err := install(o); err != nil {
			report(progress{Error: "Installation failed: " + err.Error()})
			return
		}
		installed = o
		report(progress{Percent: 100, Status: "Done", Finished: true})
	}()
}

func opUninstall() {
	go func() {
		busy.Lock()
		defer busy.Unlock()
		if err := uninstall(installDirArg); err != nil {
			report(progress{Error: "Some files could not be removed: " + err.Error()})
			return
		}
		report(progress{Percent: 100, Status: "Done", Finished: true})
	}()
}

func opFinish(raw string) {
	var f struct {
		Launch  bool `json:"launch"`
		Default bool `json:"default"`
	}
	_ = json.Unmarshal([]byte(raw), &f)
	if f.Default && installed.Assoc {
		shellOpen("ms-settings:defaultapps?registeredAppUser=OpenPlayer")
	}
	if f.Launch {
		cmd := exec.Command(filepath.Join(installed.Dir, "openplayer.exe"))
		cmd.Dir = installed.Dir
		_ = cmd.Start()
	}
}

func opClose()              { wv.Dispatch(func() { wv.Terminate() }) }
func opOpenFolder(d string) { shellOpen(d) }
func opLicenses()           { shellOpen(filepath.Join(defaultDir(), "licenses")) }
func opTheme(id string)     { colorFrame(id) }

// ---- install ---------------------------------------------------------------------

func closePlayer() {
	w, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16("OpenPlayerWindow"))), 0)
	if w == 0 {
		return
	}
	pPostMessageW.Call(w, 0x0010, 0, 0) // WM_CLOSE
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if w, _, _ = pFindWindowW.Call(uintptr(unsafe.Pointer(u16("OpenPlayerWindow"))), 0); w == 0 {
			time.Sleep(300 * time.Millisecond) // let it release the engine DLL
			return
		}
	}
}

func safeJoin(root, name string) (string, error) {
	name = filepath.FromSlash(name)
	if filepath.IsAbs(name) || strings.Contains(name, "..") || strings.ContainsRune(name, ':') {
		return "", fmt.Errorf("unsafe path %q in payload", name)
	}
	return filepath.Join(root, name), nil
}

func install(o options) error {
	report(progress{Percent: 1, Status: "Closing OpenPlayer if it is running…"})
	closePlayer()

	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	// Fresh engine each install; user settings in config\ are kept.
	for _, d := range []string{"engine", "engine.prev", "engine.staging", "engine.new"} {
		if err := os.RemoveAll(filepath.Join(o.Dir, d)); err != nil {
			return fmt.Errorf("could not replace %s (is OpenPlayer still open?): %w", d, err)
		}
	}
	report(progress{Percent: 3, DoneStep: "Prepared installation folder"})

	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("installer payload is damaged: %w", err)
	}
	var total, done uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
	}
	last := time.Now()
	for _, f := range zr.File {
		dst, err := safeJoin(o.Dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			rc.Close()
			return fmt.Errorf("could not write %s: %w", f.Name, err)
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("could not write %s: %w", f.Name, err)
		}
		done += f.UncompressedSize64
		if time.Since(last) > 80*time.Millisecond {
			last = time.Now()
			report(progress{Percent: 3 + 82*float64(done)/float64(total),
				Status: fmt.Sprintf("Copying files… %d MB of %d MB", done>>20, total>>20)})
		}
	}
	report(progress{Percent: 85, Status: "Saving settings…", DoneStep: fmt.Sprintf("Copied %d files", len(zr.File))})

	if err := writeConf(filepath.Join(o.Dir, "config", "openplayer.conf"), map[string]string{
		"theme": o.Theme, "auto_update": yesNo(o.Update),
	}); err != nil {
		return err
	}
	self, err := os.Executable()
	if err == nil {
		if b, err := os.ReadFile(self); err == nil {
			_ = os.WriteFile(filepath.Join(o.Dir, "Uninstall.exe"), b, 0o755)
		}
	}
	report(progress{Percent: 90, Status: "Registering with Windows…", DoneStep: "Saved your settings"})

	if err := register(o); err != nil {
		return err
	}
	report(progress{Percent: 95, Status: "Creating shortcuts…", DoneStep: "Registered with Windows"})

	exe := filepath.Join(o.Dir, "openplayer.exe")
	if o.Start {
		if p, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0); err == nil {
			_ = shortcut(filepath.Join(p, "OpenPlayer.lnk"), exe, o.Dir)
		}
	}
	if o.Desktop {
		if p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0); err == nil {
			_ = shortcut(filepath.Join(p, "OpenPlayer.lnk"), exe, o.Dir)
		}
	}
	pSHChange.Call(0x08000000, 0, 0, 0) // SHCNE_ASSOCCHANGED
	report(progress{Percent: 99, DoneStep: "Created shortcuts"})
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func setStr(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("registry %s: %w", path, err)
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func deleteTree(path string) {
	pRegDeleteTree.Call(uintptr(registry.CURRENT_USER), uintptr(unsafe.Pointer(u16(path))))
}

func deleteValue(path, name string) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(name)
		k.Close()
	}
}

func register(o options) error {
	exe := filepath.Join(o.Dir, "openplayer.exe")
	open := `"` + exe + `" "%1"`
	enq := `"` + exe + `" --enqueue "%1"`
	icon := exe + ",0"
	steps := []struct{ path, name, value string }{
		{`Software\Classes\` + progID, "", "Media file"},
		{`Software\Classes\` + progID + `\DefaultIcon`, "", icon},
		{`Software\Classes\` + progID + `\shell\open\command`, "", open},
		{`Software\Classes\Applications\openplayer.exe\shell\open\command`, "", open},
		// Show "OpenPlayer" (not "openplayer.exe") in "Open with" and Default apps.
		{`Software\Classes\Applications\openplayer.exe`, "FriendlyAppName", "OpenPlayer"},
		{`Software\Classes\` + progID, "FriendlyTypeName", "OpenPlayer media file"},
		{`Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache`, exe + ".FriendlyAppName", "OpenPlayer"},
		{`Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache`, exe + ".ApplicationCompany", "OpenPlayer"},
		{appKey, "InstallDir", o.Dir},
		{uninstallKey, "DisplayName", "OpenPlayer"},
		{uninstallKey, "DisplayVersion", version},
		{uninstallKey, "Publisher", "OpenPlayer (open source)"},
		{uninstallKey, "DisplayIcon", exe},
		{uninstallKey, "InstallLocation", o.Dir},
		{uninstallKey, "UninstallString", `"` + filepath.Join(o.Dir, "Uninstall.exe") + `" --uninstall`},
	}
	for _, s := range steps {
		if err := setStr(s.path, s.name, s.value); err != nil {
			return err
		}
	}
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE); err == nil {
		_ = k.SetDWordValue("NoModify", 1)
		_ = k.SetDWordValue("NoRepair", 1)
		k.Close()
	}
	exts := mediaext.Media()
	if o.Assoc {
		_ = setStr(appKey+`\Capabilities`, "ApplicationName", "OpenPlayer")
		_ = setStr(appKey+`\Capabilities`, "ApplicationDescription", "Fast, clean, open-source media player")
		_ = setStr(`Software\RegisteredApplications`, "OpenPlayer", appKey+`\Capabilities`)
		for _, e := range exts {
			_ = setStr(`Software\Classes\.`+e+`\OpenWithProgids`, progID, "")
			_ = setStr(appKey+`\Capabilities\FileAssociations`, "."+e, progID)
			_ = setStr(`Software\Classes\Applications\openplayer.exe\SupportedTypes`, "."+e, "")
		}
	}
	if o.Context {
		verbs := func(base, play, add string) {
			_ = setStr(base+`\OpenPlayer.Play`, "", play)
			_ = setStr(base+`\OpenPlayer.Play`, "Icon", icon)
			_ = setStr(base+`\OpenPlayer.Play\command`, "", open)
			_ = setStr(base+`\OpenPlayer.Enqueue`, "", add)
			_ = setStr(base+`\OpenPlayer.Enqueue`, "Icon", icon)
			_ = setStr(base+`\OpenPlayer.Enqueue\command`, "", enq)
		}
		for _, e := range exts {
			verbs(`Software\Classes\SystemFileAssociations\.`+e+`\shell`, "Play with OpenPlayer", "Add to OpenPlayer playlist")
		}
		verbs(`Software\Classes\Directory\shell`, "Play folder with OpenPlayer", "Add folder to OpenPlayer playlist")
	}
	return nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func hiddenCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c
}

// shortcut creates a .lnk through the Windows Script Host COM object.
func shortcut(lnk, target, workdir string) error {
	script := "$s=(New-Object -ComObject WScript.Shell).CreateShortcut(" + psQuote(lnk) + ");" +
		"$s.TargetPath=" + psQuote(target) + ";$s.WorkingDirectory=" + psQuote(workdir) + ";" +
		"$s.IconLocation=" + psQuote(target+",0") + ";$s.Save()"
	return hiddenCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).Run()
}

// ---- uninstall ---------------------------------------------------------------

var installDirArg string

func uninstall(dir string) error {
	report(progress{Percent: 5, Status: "Closing OpenPlayer if it is running…"})
	closePlayer()
	report(progress{Percent: 20, Status: "Removing registrations…"})
	for _, e := range mediaext.Media() {
		deleteValue(`Software\Classes\.`+e+`\OpenWithProgids`, progID)
		deleteTree(`Software\Classes\SystemFileAssociations\.` + e + `\shell\OpenPlayer.Play`)
		deleteTree(`Software\Classes\SystemFileAssociations\.` + e + `\shell\OpenPlayer.Enqueue`)
	}
	for _, k := range []string{`Software\Classes\` + progID, `Software\Classes\Applications\openplayer.exe`,
		`Software\Classes\Directory\shell\OpenPlayer.Play`, `Software\Classes\Directory\shell\OpenPlayer.Enqueue`,
		appKey, uninstallKey} {
		deleteTree(k)
	}
	deleteValue(`Software\RegisteredApplications`, "OpenPlayer")
	exe := filepath.Join(dir, "openplayer.exe")
	deleteValue(`Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache`, exe+".FriendlyAppName")
	deleteValue(`Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache`, exe+".ApplicationCompany")
	_ = os.RemoveAll(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer", "WebView")) // side-panel browser cache
	_ = os.Remove(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenPlayer"))
	report(progress{Percent: 45, DoneStep: "Removed Windows registrations"})

	for _, f := range []windows.KNOWNFOLDERID{*windows.FOLDERID_Programs, *windows.FOLDERID_Desktop} {
		f := f
		if p, err := windows.KnownFolderPath(&f, 0); err == nil {
			_ = os.Remove(filepath.Join(p, "OpenPlayer.lnk"))
		}
	}
	report(progress{Percent: 55, Status: "Removing files…", DoneStep: "Removed shortcuts"})

	var errs []string
	for _, n := range []string{"openplayer.exe", "enginectl.exe", "Uninstall.exe", "engine", "engine.prev",
		"engine.staging", "engine.new", "config", "licenses"} {
		if err := os.RemoveAll(filepath.Join(dir, n)); err != nil {
			errs = append(errs, n)
		}
	}
	_ = os.Remove(dir) // only if now empty; never a recursive delete of the folder itself
	pSHChange.Call(0x08000000, 0, 0, 0)
	report(progress{Percent: 98, DoneStep: "Removed program files"})
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, ", "))
	}
	return nil
}

// ---- window ----------------------------------------------------------------------

var frameColors = map[string][3]uint32{ // caption, text, border (0x00BBGGRR)
	"black-gold":     {0x00070B0E, 0x00D4EAF6, 0x0018435A},
	"aurora-night":   {0x0024140C, 0x00F9F5F1, 0x009A3B3B},
	"honey":          {0x00BEE3F5, 0x0010283B, 0x004AA5D9},
	"burgundy":       {0x00190F2B, 0x00EEE9F8, 0x00452E7A},
	"aurora-glass":   {0x00FBF7F6, 0x002A170F, 0x00FCB4A5},
	"retro-white":    {0x00EAF1F4, 0x00111111, 0x00222222},
	"midnight-glass": {0x001C0F08, 0x00F9F5F1, 0x00553A2A},
	"aurora":         {0x001C0D10, 0x00FFF3F5, 0x006B3D4A},
	"frost":          {0x00FFFAF7, 0x002A170F, 0x007A6452},
	"oled-black":     {0x00000000, 0x00E2E2E2, 0x00303030},
	"high-contrast":  {0x00000000, 0x00FFFFFF, 0x00FFFFFF},
}

func colorFrame(theme string) {
	c, ok := frameColors[theme]
	if !ok || hwnd == 0 {
		return
	}
	dark := uint32(1)
	if theme == "honey" || theme == "frost" || theme == "retro-white" || theme == "aurora-glass" {
		dark = 0
	}
	pDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	for i, attr := range []uintptr{35, 36, 34} { // caption, text, border colors (Windows 11)
		v := c[i]
		pDwmSetAttr.Call(hwnd, attr, uintptr(unsafe.Pointer(&v)), 4)
	}
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size          uint32
	Monitor, Work winRect
	Flags         uint32
}

// fitWindow sizes the window to 1000x720 *logical* pixels (scaled for the
// display's DPI), never larger than the screen's work area, and centers it.
func fitWindow() {
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	w, h := int32(1000*dpi/96), int32(720*dpi/96)
	mon, _, _ := user32.NewProc("MonitorFromWindow").Call(hwnd, 2)
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := user32.NewProc("GetMonitorInfoW").Call(mon, uintptr(unsafe.Pointer(&mi))); r != 0 {
		ww, wh := mi.Work.Right-mi.Work.Left, mi.Work.Bottom-mi.Work.Top
		w, h = min(w, ww*95/100), min(h, wh*95/100)
		x, y := mi.Work.Left+(ww-w)/2, mi.Work.Top+(wh-h)/2
		user32.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0004|0x0010) // NOZORDER|NOACTIVATE
		return
	}
	wv.SetSize(int(w), int(h), webview2.HintFixed)
}

func parseArgs() (fromTemp bool) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--uninstall":
			mode = "uninstall"
		case "--from-temp":
			fromTemp = true
		case "--dir":
			if i+1 < len(args) {
				installDirArg = args[i+1]
				i++
			}
		}
	}
	return
}

func main() {
	runtime.LockOSThread()
	fromTemp := parseArgs()
	self, _ := os.Executable()

	if mode == "uninstall" && installDirArg == "" {
		installDirArg = filepath.Dir(self)
	}
	// Uninstall.exe cannot delete itself: re-run from a temporary copy.
	if mode == "uninstall" && !fromTemp {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("openplayer-uninstall-%d.exe", os.Getpid()))
		if b, err := os.ReadFile(self); err == nil && os.WriteFile(tmp, b, 0o755) == nil {
			if exec.Command(tmp, "--uninstall", "--from-temp", "--dir", installDirArg).Start() == nil {
				return
			}
		}
	}

	dataDir, _ := os.MkdirTemp("", "openplayer-setup-webview")
	defer os.RemoveAll(dataDir)
	wv = webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  dataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: "OpenPlayer Setup", Width: 1000, Height: 720, IconId: 1, Center: true,
		},
	})
	if wv == nil {
		if msgBox("OpenPlayer Setup needs the Microsoft Edge WebView2 Runtime, which is missing on this PC.\n\nOpen the download page now?", 0x4|0x30) == 6 {
			shellOpen(webview2URL)
		}
		return
	}
	hwnd = uintptr(wv.Window())
	mica := uint32(2)
	pDwmSetAttr.Call(hwnd, 38, uintptr(unsafe.Pointer(&mica)), 4)
	colorFrame("aurora-glass")
	fitWindow()

	for name, fn := range map[string]any{
		"opInfo": opInfo, "opBrowse": opBrowse, "opInstall": opInstall, "opUninstall": opUninstall,
		"opFinish": opFinish, "opClose": opClose, "opOpenFolder": opOpenFolder, "opLicenses": opLicenses,
		"opTheme": opTheme,
	} {
		_ = wv.Bind(name, fn)
	}
	wv.SetHtml(uiHTML)
	wv.Run()
	wv.Destroy()

	if mode == "uninstall" && fromTemp {
		// Delete this temporary copy a moment after exiting.
		_ = hiddenCmd("cmd.exe", "/c", "ping 127.0.0.1 -n 3 >nul & del /f /q \""+self+"\"").Start()
	}
}
