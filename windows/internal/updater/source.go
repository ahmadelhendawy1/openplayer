package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openplayer/internal/manifest"
)

// Source fetches release files. rel is one of "manifest.json",
// "manifest.json.sig" or "files/<sha256>" — nothing else is ever requested.
type Source interface {
	Get(ctx context.Context, rel string) (io.ReadCloser, error)
}

func checkRel(rel string) error {
	switch rel {
	case "manifest.json", "manifest.json.sig":
		return nil
	}
	if h, ok := strings.CutPrefix(rel, "files/"); ok && manifest.ValidHash(h) {
		return nil
	}
	return fmt.Errorf("illegal release path %q", rel)
}

// DirSource reads a release from a local directory (offline updates, tests).
type DirSource struct{ Root string }

func (d DirSource) Get(_ context.Context, rel string) (io.ReadCloser, error) {
	if err := checkRel(rel); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(d.Root, filepath.FromSlash(rel)))
}

// HTTPSource reads a release over HTTPS. Plain HTTP is refused unless
// AllowHTTP is set (tests only). With Flat, "files/<sha>" is fetched as
// "<base>/<sha>", which fits GitHub release assets (no sub-folders).
type HTTPSource struct {
	Base      string
	Flat      bool
	AllowHTTP bool
	Client    *http.Client
}

func (h HTTPSource) Get(ctx context.Context, rel string) (io.ReadCloser, error) {
	if err := checkRel(rel); err != nil {
		return nil, err
	}
	if h.Flat {
		rel = strings.TrimPrefix(rel, "files/")
	}
	u, err := url.Parse(strings.TrimRight(h.Base, "/") + "/" + rel)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(h.AllowHTTP && u.Scheme == "http") {
		return nil, fmt.Errorf("refusing non-HTTPS source %q", u.Scheme)
	}
	c := h.Client
	if c == nil {
		c = &http.Client{
			Timeout: 10 * time.Minute,
			// GitHub redirects downloads to its CDN; never follow a downgrade to http.
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				if r.URL.Scheme != "https" && !h.AllowHTTP {
					return fmt.Errorf("refusing redirect to %s", r.URL.Scheme)
				}
				if len(via) > 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "openplayer-updater")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rel, resp.Status)
	}
	return resp.Body, nil
}

// NewSource picks a source from a string: https:// URL or a directory.
func NewSource(s string) (Source, error) {
	if strings.HasPrefix(s, "https://") {
		return HTTPSource{Base: s}, nil
	}
	if strings.HasPrefix(s, "http://") {
		return nil, fmt.Errorf("plain http:// sources are not allowed")
	}
	st, err := os.Stat(s)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("source %q is not an https URL or a directory", s)
	}
	return DirSource{Root: s}, nil
}
