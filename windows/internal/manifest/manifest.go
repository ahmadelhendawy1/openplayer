// Package manifest defines the signed engine manifest: the list of engine
// files (FFmpeg, libmpv, libass, ...) a release consists of, with hashes.
//
// A manifest is published as two files:
//
//	manifest.json      exact bytes that were signed
//	manifest.json.sig  base64 ed25519 signature over those bytes
//
// Component files are stored content-addressed at files/<sha256>, so a
// manifest never contains a URL that an attacker could redirect.
package manifest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	SchemaVersion   = 1
	MaxManifestSize = 1 << 20   // 1 MiB
	MaxFileSize     = 512 << 20 // 512 MiB per component file
)

// Component is one file of the engine.
type Component struct {
	Name    string `json:"name"`    // e.g. "ffmpeg-avcodec"
	Version string `json:"version"` // upstream version, e.g. "7.1.1"
	Path    string `json:"path"`    // install path relative to the engine dir
	SHA256  string `json:"sha256"`  // lowercase hex
	Size    int64  `json:"size"`
	License string `json:"license"` // SPDX id, e.g. "LGPL-2.1-or-later"
}

// Manifest describes one engine release.
type Manifest struct {
	Schema        int         `json:"schema"`
	Serial        int64       `json:"serial"` // strictly increasing; blocks downgrades
	Channel       string      `json:"channel"`
	EngineVersion string      `json:"engine_version"`
	Released      string      `json:"released"`
	MinUpdater    int         `json:"min_updater"`
	Components    []Component `json:"components"`
}

// Engine DLLs, license texts, and the controls skin (Lua scripts, JSON
// translations, fonts). Never executables or other code formats.
var allowedExt = map[string]bool{
	".dll": true, ".txt": true, ".lua": true, ".json": true, ".otf": true, ".ttf": true,
}

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SafePath rejects any install path that could escape the engine directory
// or misbehave on Windows. Only forward slashes and allowlisted file types.
func SafePath(p string) error {
	if p == "" || len(p) > 200 {
		return errors.New("empty or too long path")
	}
	if strings.ContainsAny(p, "\\:*?\"<>|\x00") || strings.HasPrefix(p, "/") {
		return fmt.Errorf("illegal character or absolute path: %q", p)
	}
	for _, r := range p {
		if r < 0x20 {
			return fmt.Errorf("control character in path: %q", p)
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("illegal path segment in %q", p)
		}
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return fmt.Errorf("segment ends with dot or space: %q", p)
		}
		base := seg
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if reservedNames[strings.ToUpper(base)] {
			return fmt.Errorf("reserved Windows name in %q", p)
		}
	}
	if !allowedExt[strings.ToLower(path.Ext(p))] {
		return fmt.Errorf("file type not allowed: %q", p)
	}
	return nil
}

// ValidHash reports whether s is a lowercase 64-char hex sha256.
func ValidHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && s == strings.ToLower(s)
}

// Parse strictly decodes and validates manifest bytes. It does NOT check the
// signature; call Verify on the same bytes first.
func Parse(data []byte) (*Manifest, error) {
	if len(data) > MaxManifestSize {
		return nil, errors.New("manifest too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("manifest json: trailing data")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks structural rules.
func (m *Manifest) Validate() error {
	if m.Schema != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema %d", m.Schema)
	}
	if m.Serial <= 0 {
		return errors.New("serial must be positive")
	}
	if m.Channel == "" || m.EngineVersion == "" {
		return errors.New("channel and engine_version are required")
	}
	if len(m.Components) == 0 || len(m.Components) > 500 {
		return errors.New("component count out of range")
	}
	seen := map[string]bool{}
	for _, c := range m.Components {
		if c.Name == "" || c.Version == "" || c.License == "" {
			return fmt.Errorf("component %q: name, version and license are required", c.Name)
		}
		if err := SafePath(c.Path); err != nil {
			return fmt.Errorf("component %q: %w", c.Name, err)
		}
		key := strings.ToLower(c.Path) // Windows is case-insensitive
		if seen[key] {
			return fmt.Errorf("duplicate path %q", c.Path)
		}
		seen[key] = true
		if !ValidHash(c.SHA256) {
			return fmt.Errorf("component %q: bad sha256", c.Name)
		}
		if c.Size < 0 || c.Size > MaxFileSize {
			return fmt.Errorf("component %q: bad size", c.Name)
		}
	}
	return nil
}

// Sign returns the base64 ed25519 signature of data.
func Sign(priv ed25519.PrivateKey, data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
}

// Verify checks a base64 signature over data.
func Verify(pub ed25519.PublicKey, data, sigB64 []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("malformed signature")
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, data, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
