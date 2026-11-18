// Package updater keeps the player's engine directory (libmpv, FFmpeg,
// libass, ...) up to date from a signed release.
//
// On-disk layout under Root:
//
//	engine/          live engine files + manifest.json + manifest.json.sig
//	engine.staging/  verified downloads waiting to be applied (READY marker)
//	engine.prev/     previous engine, kept for rollback
//
// Download never touches the live engine. Apply swaps directories and must
// run before the player loads the engine DLLs (Windows locks loaded DLLs).
package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"openplayer/internal/manifest"
)

// Version is the updater's own capability level; manifests may demand more.
const Version = 1

var (
	ErrDowngrade   = errors.New("release is older than the installed engine (downgrade refused)")
	ErrNeedUpdater = errors.New("release requires a newer updater; update the player first")
	ErrNotStaged   = errors.New("no staged update is ready")
)

// Updater operates on one installation root.
type Updater struct {
	Root   string
	PubKey ed25519.PublicKey
	Src    Source
}

// Plan is the result of Check.
type Plan struct {
	Manifest   *manifest.Manifest
	Raw, Sig   []byte
	FromSerial int64 // 0 when nothing is installed
	UpToDate   bool
	Fetch      []manifest.Component // must be downloaded
	Reuse      []manifest.Component // already present and verified locally
	Remove     []string             // installed paths that disappear
}

func (u *Updater) live() string    { return filepath.Join(u.Root, "engine") }
func (u *Updater) staging() string { return filepath.Join(u.Root, "engine.staging") }
func (u *Updater) prev() string    { return filepath.Join(u.Root, "engine.prev") }
func (u *Updater) next() string    { return filepath.Join(u.Root, "engine.new") }

// Recover repairs an interrupted Apply or Rollback. Safe to call any time.
func (u *Updater) Recover() error {
	_ = os.RemoveAll(u.next())
	_ = os.RemoveAll(filepath.Join(u.Root, "engine.rollback"))
	if _, err := os.Stat(u.live()); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(u.prev()); err == nil {
			return os.Rename(u.prev(), u.live())
		}
	}
	return nil
}

// Installed returns the verified manifest of the live engine, or nil if none.
func (u *Updater) Installed() (*manifest.Manifest, error) {
	return readSigned(u.live(), u.PubKey)
}

func readSigned(dir string, pub ed25519.PublicKey) (*manifest.Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		return nil, err
	}
	if err := manifest.Verify(pub, raw, sig); err != nil {
		return nil, fmt.Errorf("installed manifest: %w", err)
	}
	return manifest.Parse(raw)
}

func readAll(ctx context.Context, src Source, rel string, max int64) ([]byte, error) {
	rc, err := src.Get(ctx, rel)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s exceeds %d bytes", rel, max)
	}
	return b, nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Check fetches and verifies the latest manifest and plans the update.
func (u *Updater) Check(ctx context.Context) (*Plan, error) {
	if err := u.Recover(); err != nil {
		return nil, err
	}
	raw, err := readAll(ctx, u.Src, "manifest.json", manifest.MaxManifestSize)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sig, err := readAll(ctx, u.Src, "manifest.json.sig", 4096)
	if err != nil {
		return nil, fmt.Errorf("fetch signature: %w", err)
	}
	if err := manifest.Verify(u.PubKey, raw, sig); err != nil {
		return nil, err
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		return nil, err
	}
	if m.MinUpdater > Version {
		return nil, ErrNeedUpdater
	}
	cur, err := u.Installed()
	if err != nil {
		return nil, err
	}
	p := &Plan{Manifest: m, Raw: raw, Sig: sig}
	oldPaths := map[string]bool{}
	if cur != nil {
		p.FromSerial = cur.Serial
		if m.Serial < cur.Serial {
			return nil, ErrDowngrade
		}
		if m.Serial == cur.Serial {
			p.UpToDate = true
		}
		for _, c := range cur.Components {
			oldPaths[c.Path] = true
		}
	}
	newPaths := map[string]bool{}
	for _, c := range m.Components {
		newPaths[c.Path] = true
		h, err := hashFile(filepath.Join(u.live(), filepath.FromSlash(c.Path)))
		if cur != nil && err == nil && h == c.SHA256 {
			p.Reuse = append(p.Reuse, c)
		} else {
			p.Fetch = append(p.Fetch, c)
		}
	}
	for old := range oldPaths {
		if !newPaths[old] {
			p.Remove = append(p.Remove, old)
		}
	}
	return p, nil
}

// Download stages every file the plan needs, verifying size and hash while
// streaming. The live engine is never modified. READY is written last.
func (u *Updater) Download(ctx context.Context, p *Plan) error {
	if p.UpToDate {
		return nil
	}
	_ = os.RemoveAll(u.staging())
	files := filepath.Join(u.staging(), "files")
	if err := os.MkdirAll(files, 0o755); err != nil {
		return err
	}
	fail := func(err error) error { _ = os.RemoveAll(u.staging()); return err }
	done := map[string]bool{}
	for _, c := range p.Fetch {
		if done[c.SHA256] {
			continue
		}
		done[c.SHA256] = true
		if err := u.fetchFile(ctx, c, files); err != nil {
			return fail(fmt.Errorf("%s: %w", c.Name, err))
		}
	}
	if err := os.WriteFile(filepath.Join(u.staging(), "manifest.json"), p.Raw, 0o644); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(u.staging(), "manifest.json.sig"), p.Sig, 0o644); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(u.staging(), "READY"), nil, 0o644); err != nil {
		return fail(err)
	}
	return nil
}

func (u *Updater) fetchFile(ctx context.Context, c manifest.Component, dir string) error {
	rc, err := u.Src.Get(ctx, "files/"+c.SHA256)
	if err != nil {
		return err
	}
	defer rc.Close()
	part := filepath.Join(dir, c.SHA256+".part")
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(rc, c.Size+1))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	if n != c.Size {
		os.Remove(part)
		return fmt.Errorf("size mismatch: got %d, want %d", n, c.Size)
	}
	if hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		os.Remove(part)
		return errors.New("sha256 mismatch")
	}
	return os.Rename(part, filepath.Join(dir, c.SHA256))
}

// Apply swaps the staged update into place. Call before loading the engine.
func (u *Updater) Apply() (*manifest.Manifest, error) {
	if err := u.Recover(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(u.staging(), "READY")); err != nil {
		return nil, ErrNotStaged
	}
	m, err := readSigned(u.staging(), u.PubKey)
	if err != nil {
		return nil, fmt.Errorf("staged manifest: %w", err)
	}
	if m == nil {
		return nil, ErrNotStaged
	}
	cur, err := u.Installed()
	if err != nil {
		return nil, err
	}
	if cur != nil && m.Serial <= cur.Serial {
		_ = os.RemoveAll(u.staging())
		return nil, ErrDowngrade
	}
	_ = os.RemoveAll(u.next())
	if err := os.MkdirAll(u.next(), 0o755); err != nil {
		return nil, err
	}
	fail := func(err error) (*manifest.Manifest, error) { _ = os.RemoveAll(u.next()); return nil, err }
	for _, c := range m.Components {
		dst := filepath.Join(u.next(), filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fail(err)
		}
		src := filepath.Join(u.staging(), "files", c.SHA256)
		if _, err := os.Stat(src); err != nil {
			src = filepath.Join(u.live(), filepath.FromSlash(c.Path)) // unchanged file
		}
		if err := copyVerified(src, dst, c); err != nil {
			return fail(fmt.Errorf("%s: %w", c.Name, err))
		}
	}
	if err := os.WriteFile(filepath.Join(u.next(), "manifest.json"), mustRead(u.staging(), "manifest.json"), 0o644); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(u.next(), "manifest.json.sig"), mustRead(u.staging(), "manifest.json.sig"), 0o644); err != nil {
		return fail(err)
	}
	// Swap: live -> prev, next -> live. Roll back the first step on failure.
	hadLive := false
	if _, err := os.Stat(u.live()); err == nil {
		hadLive = true
		_ = os.RemoveAll(u.prev())
		if err := os.Rename(u.live(), u.prev()); err != nil {
			return fail(fmt.Errorf("engine is in use (close the player and retry): %w", err))
		}
	}
	if err := os.Rename(u.next(), u.live()); err != nil {
		if hadLive {
			_ = os.Rename(u.prev(), u.live())
		}
		return fail(err)
	}
	_ = os.RemoveAll(u.staging())
	return m, nil
}

func mustRead(dir, name string) []byte {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return b
}

func copyVerified(src, dst string, c manifest.Component) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	cerr := out.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != c.Size || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return errors.New("file does not match manifest")
	}
	return nil
}

// Rollback swaps engine.prev back in. The replaced engine becomes the new
// engine.prev, so Rollback twice returns to where you started.
func (u *Updater) Rollback() (*manifest.Manifest, error) {
	if err := u.Recover(); err != nil {
		return nil, err
	}
	if _, err := readSigned(u.prev(), u.PubKey); err != nil {
		return nil, fmt.Errorf("previous engine unusable: %w", err)
	}
	if _, err := os.Stat(u.prev()); err != nil {
		return nil, errors.New("no previous engine to roll back to")
	}
	tmp := filepath.Join(u.Root, "engine.rollback")
	if err := os.Rename(u.live(), tmp); err != nil {
		return nil, fmt.Errorf("engine is in use (close the player and retry): %w", err)
	}
	if err := os.Rename(u.prev(), u.live()); err != nil {
		_ = os.Rename(tmp, u.live())
		return nil, err
	}
	if err := os.Rename(tmp, u.prev()); err != nil {
		return nil, err
	}
	return u.Installed()
}

// Verify re-hashes every installed file against the signed installed manifest.
func (u *Updater) Verify() ([]string, error) {
	m, err := u.Installed()
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("no engine installed")
	}
	var bad []string
	for _, c := range m.Components {
		h, err := hashFile(filepath.Join(u.live(), filepath.FromSlash(c.Path)))
		switch {
		case err != nil:
			bad = append(bad, c.Path+": missing")
		case h != c.SHA256:
			bad = append(bad, c.Path+": modified")
		}
	}
	return bad, nil
}

// Describe renders a plan for humans.
func (p *Plan) Describe() string {
	var b strings.Builder
	switch {
	case p.UpToDate:
		fmt.Fprintf(&b, "up to date (engine %s, serial %d)\n", p.Manifest.EngineVersion, p.Manifest.Serial)
	default:
		fmt.Fprintf(&b, "update available: serial %d -> %d (engine %s)\n", p.FromSerial, p.Manifest.Serial, p.Manifest.EngineVersion)
		for _, c := range p.Fetch {
			fmt.Fprintf(&b, "  download %-28s %-10s %d bytes\n", c.Name, c.Version, c.Size)
		}
		for _, r := range p.Remove {
			fmt.Fprintf(&b, "  remove   %s\n", r)
		}
		fmt.Fprintf(&b, "  unchanged: %d file(s)\n", len(p.Reuse))
	}
	return b.String()
}
