// Package pack builds a publishable engine release: a signed manifest plus
// content-addressed component files. Used by the release pipeline and tests.
package pack

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"openplayer/internal/manifest"
)

// Entry describes one input file.
type Entry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	File    string `json:"file"`           // file or directory, relative to the input dir
	Path    string `json:"path,omitempty"` // install path (or prefix for a directory); defaults to File
	License string `json:"license"`
}

// Options controls a pack run.
type Options struct {
	InDir         string
	OutDir        string
	Entries       []Entry
	Serial        int64
	Channel       string
	EngineVersion string
	MinUpdater    int
	Released      time.Time // zero = now
	Key           ed25519.PrivateKey
}

// expand replaces directory entries with one entry per file inside them.
func expand(in string, entries []Entry) ([]Entry, error) {
	var out []Entry
	for _, e := range entries {
		full := filepath.Join(in, filepath.FromSlash(e.File))
		st, err := os.Stat(full)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, e)
			continue
		}
		prefix := e.Path
		if prefix == "" {
			prefix = e.File
		}
		err = filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(full, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			out = append(out, Entry{
				Name: e.Name + "/" + rel, Version: e.Version, License: e.License,
				File: e.File + "/" + rel, Path: prefix + "/" + rel,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Run writes OutDir/manifest.json, manifest.json.sig and OutDir/files/<sha>.
func Run(o Options) (*manifest.Manifest, error) {
	if o.Channel == "" {
		o.Channel = "stable"
	}
	if o.Released.IsZero() {
		o.Released = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Join(o.OutDir, "files"), 0o755); err != nil {
		return nil, err
	}
	m := manifest.Manifest{
		Schema:        manifest.SchemaVersion,
		Serial:        o.Serial,
		Channel:       o.Channel,
		EngineVersion: o.EngineVersion,
		Released:      o.Released.UTC().Format(time.RFC3339),
		MinUpdater:    o.MinUpdater,
	}
	entries, err := expand(o.InDir, o.Entries)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(o.InDir, filepath.FromSlash(e.File)))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		h := hex.EncodeToString(sum[:])
		p := e.Path
		if p == "" {
			p = e.File
		}
		m.Components = append(m.Components, manifest.Component{
			Name: e.Name, Version: e.Version, Path: p,
			SHA256: h, Size: int64(len(data)), License: e.License,
		})
		if err := os.WriteFile(filepath.Join(o.OutDir, "files", h), data, 0o644); err != nil {
			return nil, err
		}
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to publish invalid manifest: %w", err)
	}
	raw, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(o.OutDir, "manifest.json"), raw, 0o644); err != nil {
		return nil, err
	}
	sig := manifest.Sign(o.Key, raw) + "\n"
	if err := os.WriteFile(filepath.Join(o.OutDir, "manifest.json.sig"), []byte(sig), 0o644); err != nil {
		return nil, err
	}
	return &m, nil
}
