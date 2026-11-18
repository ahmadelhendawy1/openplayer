package main

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// settings is a tiny key=value file (config\openplayer.conf). The installer
// writes the initial choices; the Tools menu changes them.
type settings struct {
	mu   sync.Mutex
	path string
	m    map[string]string
}

func loadSettings(path string) *settings {
	s := &settings{path: path, m: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			s.m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return s
}

func (s *settings) get(key, def string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[key]; ok {
		return v
	}
	return def
}

func (s *settings) flag(key string, def bool) bool {
	switch strings.ToLower(s.get(key, "")) {
	case "yes", "true", "1", "on":
		return true
	case "no", "false", "0", "off":
		return false
	}
	return def
}

// all returns a copy of every setting.
func (s *settings) all() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := make(map[string]string, len(s.m))
	for k, v := range s.m {
		c[k] = v
	}
	return c
}

// del removes a setting (and saves).
func (s *settings) del(key string) error {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
	return s.save()
}

func (s *settings) set(key, value string) error {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
	return s.save()
}

func (s *settings) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# OpenPlayer settings\r\n")
	for _, k := range keys {
		b.WriteString(k + "=" + s.m[k] + "\r\n")
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
