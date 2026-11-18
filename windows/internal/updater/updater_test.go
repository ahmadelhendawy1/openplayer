package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"openplayer/internal/manifest"
	"openplayer/internal/pack"
)

type env struct {
	t    *testing.T
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	root string
}

func newEnv(t *testing.T) *env {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &env{t: t, pub: pub, priv: priv, root: t.TempDir()}
}

// release builds a signed release dir from name->content.
func (e *env) release(serial int64, files map[string]string) string {
	in, out := e.t.TempDir(), e.t.TempDir()
	var entries []pack.Entry
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(in, name), []byte(content), 0o644); err != nil {
			e.t.Fatal(err)
		}
		entries = append(entries, pack.Entry{Name: name, Version: "1", File: name, License: "LGPL-2.1-or-later"})
	}
	if _, err := pack.Run(pack.Options{InDir: in, OutDir: out, Entries: entries, Serial: serial,
		EngineVersion: "t" + string(rune('0'+serial)), Key: e.priv}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

type countingSource struct {
	Source
	mu   sync.Mutex
	gets []string
}

func (c *countingSource) Get(ctx context.Context, rel string) (io.ReadCloser, error) {
	c.mu.Lock()
	c.gets = append(c.gets, rel)
	c.mu.Unlock()
	return c.Source.Get(ctx, rel)
}

func (e *env) updater(rel string) (*Updater, *countingSource) {
	cs := &countingSource{Source: DirSource{Root: rel}}
	return &Updater{Root: e.root, PubKey: e.pub, Src: cs}, cs
}

func (e *env) update(rel string) (*Updater, *countingSource, error) {
	u, cs := e.updater(rel)
	ctx := context.Background()
	p, err := u.Check(ctx)
	if err != nil {
		return u, cs, err
	}
	if err := u.Download(ctx, p); err != nil {
		return u, cs, err
	}
	_, err = u.Apply()
	return u, cs, err
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallThenPartialUpdate(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"libmpv-2.dll": "mpv-v1", "avcodec.dll": "av-v1", "old.dll": "gone-soon"})
	if _, _, err := e.update(v1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(e.root, "engine", "avcodec.dll")); got != "av-v1" {
		t.Fatalf("got %q", got)
	}
	// v2: avcodec changes, libmpv unchanged, old.dll removed, libass.dll added
	v2 := e.release(2, map[string]string{"libmpv-2.dll": "mpv-v1", "avcodec.dll": "av-v2", "libass.dll": "ass-v1"})
	u, cs, err := e.update(v2)
	if err != nil {
		t.Fatal(err)
	}
	var fetched []string
	for _, g := range cs.gets {
		if strings.HasPrefix(g, "files/") {
			fetched = append(fetched, g)
		}
	}
	if len(fetched) != 2 {
		t.Fatalf("expected 2 file downloads (changed + new), got %d: %v", len(fetched), fetched)
	}
	eng := filepath.Join(e.root, "engine")
	if read(t, filepath.Join(eng, "avcodec.dll")) != "av-v2" || read(t, filepath.Join(eng, "libmpv-2.dll")) != "mpv-v1" {
		t.Fatal("wrong file contents after update")
	}
	if _, err := os.Stat(filepath.Join(eng, "old.dll")); !os.IsNotExist(err) {
		t.Fatal("old.dll should be removed")
	}
	if bad, err := u.Verify(); err != nil || len(bad) != 0 {
		t.Fatalf("verify: %v %v", bad, err)
	}
	// Up to date now.
	p, err := u.Check(context.Background())
	if err != nil || !p.UpToDate {
		t.Fatalf("expected up to date, %v %v", p, err)
	}
}

func TestTamperedManifestRejected(t *testing.T) {
	e := newEnv(t)
	rel := e.release(1, map[string]string{"a.dll": "x"})
	mp := filepath.Join(rel, "manifest.json")
	b := read(t, mp)
	if err := os.WriteFile(mp, []byte(strings.Replace(b, "\"serial\": 1", "\"serial\": 9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	u, _ := e.updater(rel)
	if _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("want signature error, got %v", err)
	}
}

func TestWrongKeyRejected(t *testing.T) {
	e := newEnv(t)
	rel := e.release(1, map[string]string{"a.dll": "x"})
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	u := &Updater{Root: e.root, PubKey: otherPub, Src: DirSource{Root: rel}}
	if _, err := u.Check(context.Background()); err == nil {
		t.Fatal("expected failure with wrong public key")
	}
}

func TestCorruptedFileRejectedLiveUntouched(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"a.dll": "good-1"})
	if _, _, err := e.update(v1); err != nil {
		t.Fatal(err)
	}
	v2 := e.release(2, map[string]string{"a.dll": "good-2"})
	m, _ := manifest.Parse([]byte(read(t, filepath.Join(v2, "manifest.json"))))
	// Corrupt the stored blob (same length, different bytes).
	if err := os.WriteFile(filepath.Join(v2, "files", m.Components[0].SHA256), []byte("evil-22"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, _ := e.updater(v2)
	ctx := context.Background()
	p, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Download(ctx, p); err == nil {
		t.Fatal("expected download to fail")
	}
	if got := read(t, filepath.Join(e.root, "engine", "a.dll")); got != "good-1" {
		t.Fatalf("live engine modified: %q", got)
	}
	if _, err := u.Apply(); err != ErrNotStaged {
		t.Fatalf("apply after failed download should be ErrNotStaged, got %v", err)
	}
}

func TestDowngradeRefused(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"a.dll": "1"})
	v2 := e.release(2, map[string]string{"a.dll": "2"})
	if _, _, err := e.update(v2); err != nil {
		t.Fatal(err)
	}
	u, _ := e.updater(v1) // replayed, validly signed, older release
	if _, err := u.Check(context.Background()); err != ErrDowngrade {
		t.Fatalf("want ErrDowngrade, got %v", err)
	}
}

func TestRollbackAndBack(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"a.dll": "one"})
	v2 := e.release(2, map[string]string{"a.dll": "two"})
	e.update(v1)
	u, _, err := e.update(v2)
	if err != nil {
		t.Fatal(err)
	}
	m, err := u.Rollback()
	if err != nil || m.Serial != 1 {
		t.Fatalf("rollback: %v %v", m, err)
	}
	if read(t, filepath.Join(e.root, "engine", "a.dll")) != "one" {
		t.Fatal("rollback did not restore v1")
	}
	m, err = u.Rollback()
	if err != nil || m.Serial != 2 {
		t.Fatalf("second rollback: %v %v", m, err)
	}
}

func TestRecoverInterruptedApply(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"a.dll": "one"})
	v2 := e.release(2, map[string]string{"a.dll": "two"})
	e.update(v1)
	e.update(v2)
	// Simulate a crash between "live -> prev" and "new -> live".
	if err := os.Rename(filepath.Join(e.root, "engine"), filepath.Join(e.root, "engine.tmpcrash")); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(e.root, "engine.prev")) // Apply removes the old prev before this step
	if err := os.Rename(filepath.Join(e.root, "engine.tmpcrash"), filepath.Join(e.root, "engine.prev")); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(e.root, "engine.new"), 0o755)
	u, _ := e.updater(v2)
	if err := u.Recover(); err != nil {
		t.Fatal(err)
	}
	m, err := u.Installed()
	if err != nil || m == nil {
		t.Fatalf("engine not restored: %v %v", m, err)
	}
	if _, err := os.Stat(filepath.Join(e.root, "engine.new")); !os.IsNotExist(err) {
		t.Fatal("engine.new should be cleaned")
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	e := newEnv(t)
	v1 := e.release(1, map[string]string{"a.dll": "clean"})
	u, _, err := e.update(v1)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(e.root, "engine", "a.dll"), []byte("patched"), 0o644)
	bad, err := u.Verify()
	if err != nil || len(bad) != 1 || !strings.Contains(bad[0], "modified") {
		t.Fatalf("verify: %v %v", bad, err)
	}
}

func TestInstalledManifestTamperDetected(t *testing.T) {
	e := newEnv(t)
	u, _, err := e.update(e.release(5, map[string]string{"a.dll": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(e.root, "engine", "manifest.json")
	os.WriteFile(mp, []byte(strings.Replace(read(t, mp), "\"serial\": 5", "\"serial\": 1", 1)), 0o644)
	if _, err := u.Installed(); err == nil {
		t.Fatal("expected tamper error on installed manifest")
	}
}

func TestUnsafePathsRejectedByParser(t *testing.T) {
	for _, p := range []string{"../evil.dll", "/abs.dll", "a\\b.dll", "C:evil.dll", "sub/../../x.dll",
		"nul.dll", "run.exe", "x.ps1", "x.bat", "a.dll.", "con/x.dll", "", "a//b.dll"} {
		if manifest.SafePath(p) == nil {
			t.Errorf("SafePath accepted %q", p)
		}
	}
	for _, p := range []string{"libmpv-2.dll", "plugins/x.dll", "licenses/ffmpeg.txt",
		"skin/uosc/main.lua", "skin/uosc/intl/de.json", "skin/fonts/uosc_icons.otf"} {
		if err := manifest.SafePath(p); err != nil {
			t.Errorf("SafePath rejected %q: %v", p, err)
		}
	}
}

func TestMalformedManifestRejected(t *testing.T) {
	good := `{"schema":1,"serial":1,"channel":"stable","engine_version":"1","released":"x","min_updater":0,"components":[{"name":"a","version":"1","path":"a.dll","sha256":"` + strings.Repeat("a", 64) + `","size":1,"license":"MIT"}]}`
	if _, err := manifest.Parse([]byte(good)); err != nil {
		t.Fatalf("good manifest rejected: %v", err)
	}
	bad := map[string]string{
		"unknown field": strings.Replace(good, `"schema":1`, `"schema":1,"extra":1`, 1),
		"trailing data": good + " {}",
		"upper hash":    strings.Replace(good, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		"bad schema":    strings.Replace(good, `"schema":1`, `"schema":2`, 1),
		"zero serial":   strings.Replace(good, `"serial":1`, `"serial":0`, 1),
		"no license":    strings.Replace(good, `"license":"MIT"`, `"license":""`, 1),
	}
	for name, doc := range bad {
		if _, err := manifest.Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNeedsNewerUpdater(t *testing.T) {
	e := newEnv(t)
	in, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(in, "a.dll"), []byte("x"), 0o644)
	pack.Run(pack.Options{InDir: in, OutDir: out, Serial: 1, EngineVersion: "1", MinUpdater: Version + 1, Key: e.priv,
		Entries: []pack.Entry{{Name: "a", Version: "1", File: "a.dll", License: "MIT"}}})
	u, _ := e.updater(out)
	if _, err := u.Check(context.Background()); err != ErrNeedUpdater {
		t.Fatalf("want ErrNeedUpdater, got %v", err)
	}
}

func TestHTTPSource(t *testing.T) {
	e := newEnv(t)
	rel := e.release(1, map[string]string{"a.dll": "over-http"})
	srv := httptest.NewServer(http.FileServer(http.Dir(rel)))
	defer srv.Close()

	// Plain HTTP refused by default.
	if _, err := NewSource(srv.URL); err == nil {
		t.Fatal("NewSource accepted http://")
	}
	if _, err := (HTTPSource{Base: srv.URL}).Get(context.Background(), "manifest.json"); err == nil {
		t.Fatal("HTTPSource fetched over plain http without AllowHTTP")
	}
	u := &Updater{Root: e.root, PubKey: e.pub, Src: HTTPSource{Base: srv.URL, AllowHTTP: true}}
	ctx := context.Background()
	p, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Download(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(e.root, "engine", "a.dll")) != "over-http" {
		t.Fatal("http install failed")
	}
}

func TestSourceRejectsArbitraryPaths(t *testing.T) {
	d := DirSource{Root: t.TempDir()}
	for _, rel := range []string{"../etc/passwd", "files/../../x", "files/abc", "other.txt"} {
		if _, err := d.Get(context.Background(), rel); err == nil || strings.Contains(err.Error(), "no such file") {
			t.Errorf("DirSource did not reject %q: %v", rel, err)
		}
	}
}

func TestFlatHTTPSourceAndDirectoryPack(t *testing.T) {
	e := newEnv(t)
	in, rel := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(in, "skin", "uosc", "lib"), 0o755)
	os.WriteFile(filepath.Join(in, "libmpv-2.dll"), []byte("engine"), 0o644)
	os.WriteFile(filepath.Join(in, "skin", "uosc", "main.lua"), []byte("-- main"), 0o644)
	os.WriteFile(filepath.Join(in, "skin", "uosc", "lib", "utils.lua"), []byte("-- utils"), 0o644)
	m, err := pack.Run(pack.Options{InDir: in, OutDir: rel, Serial: 1, EngineVersion: "1", Key: e.priv,
		Entries: []pack.Entry{
			{Name: "libmpv", Version: "1", File: "libmpv-2.dll", License: "LGPL-2.1-or-later"},
			{Name: "uosc", Version: "5", File: "skin/uosc", License: "LGPL-2.1-only"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Components) != 3 {
		t.Fatalf("want 3 components, got %d", len(m.Components))
	}
	// Publish flat, like GitHub release assets: <sha> next to manifest.json.
	flat := t.TempDir()
	for _, n := range []string{"manifest.json", "manifest.json.sig"} {
		b, _ := os.ReadFile(filepath.Join(rel, n))
		os.WriteFile(filepath.Join(flat, n), b, 0o644)
	}
	for _, c := range m.Components {
		b, _ := os.ReadFile(filepath.Join(rel, "files", c.SHA256))
		os.WriteFile(filepath.Join(flat, c.SHA256), b, 0o644)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(flat)))
	defer srv.Close()
	u := &Updater{Root: e.root, PubKey: e.pub, Src: HTTPSource{Base: srv.URL, Flat: true, AllowHTTP: true}}
	ctx := context.Background()
	p, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Download(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(e.root, "engine", "skin", "uosc", "lib", "utils.lua")) != "-- utils" {
		t.Fatal("skin file not installed")
	}
}
