package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

/*
The page needs mermaid, a highlighter and two typefaces, and a tool that runs on
a laptop should not call out to a CDN every time it opens. All of them go through
here: fetched once, written to a cache directory outside the repository, served
from this origin afterwards. After the first run the walkthrough works on a plane.

Nothing here is required. A page that cannot get mermaid shows the diagram source
instead of the diagram, one that cannot get the highlighter shows plain code, and
one that cannot get the fonts falls back to the system stack, which is what the
design names as its fallback anyway.
*/

const (
	vendorPrefix = "/vendor/"
	mermaidURL   = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"
	hljsURL      = "https://cdn.jsdelivr.net/npm/@highlightjs/cdn-assets@11/highlight.min.js"
	fontsURL     = "https://fonts.googleapis.com/css2?family=Sora:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap"
	// Google serves woff2 only to a user agent it believes can read it.
	fontUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

type Vendor struct {
	Dir     string
	Offline bool

	mu    sync.Mutex
	fonts map[string]string // hash -> the gstatic URL it came from
}

func NewVendor(offline bool) *Vendor {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return &Vendor{Dir: filepath.Join(dir, "code-walkthrough", "vendor"), Offline: offline, fonts: map[string]string{}}
}

func (v *Vendor) path(name string) string { return filepath.Join(v.Dir, name) }

// Prewarm pulls what the page needs before the browser asks for it, so the
// first open is not the slow one. Failures are silent on purpose: the page
// works without any of this.
func (v *Vendor) Prewarm() {
	go func() {
		if _, err := v.mermaid(); err != nil {
			return
		}
		if _, err := v.hljs(); err != nil {
			return
		}
		css, err := v.fontCSS()
		if err != nil {
			return
		}
		for _, hash := range fontHashes(css) {
			_, _ = v.font(hash)
		}
	}()
}

func (v *Vendor) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, vendorPrefix)
		switch {
		case name == "mermaid.js":
			body, err := v.mermaid()
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			serveCached(w, "application/javascript; charset=utf-8", body)
		case name == "hljs.js":
			body, err := v.hljs()
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			serveCached(w, "application/javascript; charset=utf-8", body)
		case name == "fonts.css":
			body, err := v.fontCSS()
			if err != nil {
				// An empty stylesheet is a working stylesheet. The page has a
				// fallback stack, and a 503 here would only fill the console.
				serveCached(w, "text/css; charset=utf-8", []byte("/* the typefaces could not be fetched, falling back to the system stack */\n"))
				return
			}
			serveCached(w, "text/css; charset=utf-8", body)
		case strings.HasPrefix(name, "font/"):
			body, err := v.font(strings.TrimSuffix(strings.TrimPrefix(name, "font/"), ".woff2"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			serveCached(w, "font/woff2", body)
		default:
			http.NotFound(w, r)
		}
	}
}

func serveCached(w http.ResponseWriter, ct string, body []byte) {
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

func (v *Vendor) mermaid() ([]byte, error) {
	return v.cached("mermaid-11.js", mermaidURL, nil)
}

func (v *Vendor) hljs() ([]byte, error) {
	return v.cached("highlight-11.js", hljsURL, nil)
}

var gstaticRe = regexp.MustCompile(`https://fonts\.gstatic\.com/[^)'" ]+`)

// fontCSS rewrites every font file URL onto this origin, so the browser never
// talks to Google either.
func (v *Vendor) fontCSS() ([]byte, error) {
	raw, err := v.cached("fonts.css", fontsURL, map[string]string{"User-Agent": fontUA})
	if err != nil {
		return nil, err
	}
	out := gstaticRe.ReplaceAllFunc(raw, func(u []byte) []byte {
		hash := hashOf(string(u))
		v.mu.Lock()
		v.fonts[hash] = string(u)
		v.mu.Unlock()
		return []byte(vendorPrefix + "font/" + hash + ".woff2")
	})
	return out, nil
}

func (v *Vendor) font(hash string) ([]byte, error) {
	if !isHash(hash) {
		return nil, fmt.Errorf("not a font this server handed out")
	}
	v.mu.Lock()
	src := v.fonts[hash]
	v.mu.Unlock()
	if src == "" {
		// A cached file survives a restart; the map that remembers where it came
		// from does not. Rebuild it from the stylesheet before giving up.
		if _, err := v.fontCSS(); err == nil {
			v.mu.Lock()
			src = v.fonts[hash]
			v.mu.Unlock()
		}
	}
	if src == "" {
		return nil, fmt.Errorf("unknown font")
	}
	return v.cached("font-"+hash+".woff2", src, map[string]string{"User-Agent": fontUA})
}

func fontHashes(css []byte) []string {
	var out []string
	for _, u := range gstaticRe.FindAll(css, -1) {
		out = append(out, hashOf(string(u)))
	}
	return out
}

// cached is the whole policy: disk first, network once, never again.
func (v *Vendor) cached(name, url string, headers map[string]string) ([]byte, error) {
	p := v.path(name)
	if body, err := os.ReadFile(p); err == nil && len(body) > 0 {
		return body, nil
	}
	if v.Offline {
		return nil, fmt.Errorf("%s is not in the cache and this server is offline", name)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	client := &http.Client{Timeout: 25 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s: %w", name, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 24<<20))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(v.Dir, 0o755); err == nil {
		_ = os.WriteFile(p, body, 0o644)
	}
	return body, nil
}

func (v *Vendor) Clear() (int, error) {
	entries, err := os.ReadDir(v.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if err := os.Remove(filepath.Join(v.Dir, e.Name())); err == nil {
			n++
		}
	}
	return n, nil
}

func hashOf(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func isHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
