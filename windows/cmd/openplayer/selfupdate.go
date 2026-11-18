package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const maxInstaller = 400 << 20

var errBadHash = errors.New("installer SHA-256 does not match latest.json")

// fetchInstaller downloads the installer named in l into dir and verifies its
// SHA-256. Only a verified file is left on disk (dir/l.File).
func fetchInstaller(ctx context.Context, l latestInfo, latest, dir, agent string, progress func(got, total int64)) (string, error) {
	if !l.canSelfUpdate() {
		return "", errors.New("latest.json does not name a verifiable installer")
	}
	url := installerURL(latest, l.File)
	if url == "" {
		return "", errors.New("no installer address")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, l.File)
	part := final + ".part"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", agent)
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New(resp.Status)
	}
	total := resp.ContentLength
	if total > maxInstaller {
		return "", errors.New("installer too large")
	}
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	fail := func(e error) (string, error) { f.Close(); os.Remove(part); return "", e }
	h := sha256.New()
	var got int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fail(werr)
			}
			h.Write(buf[:n])
			got += int64(n)
			if got > maxInstaller {
				return fail(errors.New("installer too large"))
			}
			if progress != nil {
				progress(got, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != l.SHA256 {
		os.Remove(part)
		return "", errBadHash
	}
	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	return final, nil
}
