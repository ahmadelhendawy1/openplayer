// Package mediaext is the single list of file types OpenPlayer handles,
// shared by the player (Open dialogs) and the installer (registration).
package mediaext

import (
	_ "embed"
	"strings"
)

//go:embed extensions.txt
var raw string

// Lists returns kind ("video", "audio", "subtitle") -> extensions without dots.
func Lists() map[string][]string {
	m := map[string][]string{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			m[k] = strings.Fields(v)
		}
	}
	return m
}

// Media returns all audio and video extensions, sorted and unique.
func Media() []string {
	m := Lists()
	seen := map[string]bool{}
	var out []string
	for _, e := range append(append([]string{}, m["video"]...), m["audio"]...) {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
