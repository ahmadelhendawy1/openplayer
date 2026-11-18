package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchInstaller(t *testing.T) {
	body := make([]byte, 3<<20)
	for i := range body {
		body[i] = byte(i * 7)
	}
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps/openplayer/OpenPlayer-Setup-9.9.9.exe" {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	dir := t.TempDir()
	l := latestInfo{Version: "9.9.9", File: "OpenPlayer-Setup-9.9.9.exe", SHA256: hex.EncodeToString(sum[:])}
	calls := 0
	p, err := fetchInstaller(context.Background(), l, srv.URL+"/apps/openplayer/latest.json", dir, "test", func(got, total int64) { calls++ })
	if err != nil || filepath.Base(p) != l.File || calls == 0 {
		t.Fatalf("p=%q err=%v calls=%d", p, err, calls)
	}
	if b, _ := os.ReadFile(p); len(b) != len(body) {
		t.Fatal("size")
	}
	l.SHA256 = hex.EncodeToString(make([]byte, 32)) // wrong hash: nothing may remain
	os.Remove(p)
	if _, err := fetchInstaller(context.Background(), l, srv.URL+"/apps/openplayer/latest.json", dir, "test", nil); err != errBadHash {
		t.Fatalf("want errBadHash, got %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left files: %v", ents)
	}
	l.File = "Missing.exe"
	l.SHA256 = hex.EncodeToString(sum[:])
	if _, err := fetchInstaller(context.Background(), l, srv.URL+"/apps/openplayer/latest.json", dir, "test", nil); err == nil {
		t.Fatal("404 accepted")
	}
}
