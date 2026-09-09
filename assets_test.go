package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const indexHead = `<link rel="stylesheet" href="/vendor/fonts.css">
<link rel="stylesheet" href="/assets/app.css">
<script type="module" src="/assets/app.js"></script>`

// Without a stamp the stylesheet and the script sit at a URL that never
// changes, and a proxy that cached them keeps handing out the previous build
// after a deploy. Cloudflare does exactly that, for four hours.
func TestAssetURLsCarryAStamp(t *testing.T) {
	got := stampAssets(indexHead, false)
	for _, want := range []string{"/assets/app.css?v=", "/assets/app.js?v=", "/vendor/fonts.css?v="} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is not stamped:\n%s", want, got)
		}
	}
	if s := assetStamp(); len(s) < 8 || s == "0" {
		t.Errorf("the stamp is %q, which says the assets could not be read", s)
	}
	// The same build has to stamp the same way, or every load is a cache miss.
	if again := stampAssets(indexHead, false); again != got {
		t.Error("two loads of one build stamped differently")
	}
	// In dev the files come off disk while the server runs, so it must not.
	if dev := stampAssets(indexHead, true); dev == got {
		t.Error("dev stamped the same as the build, so an edit would not show")
	}
}

func TestOnlyAStampedAssetIsCached(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("body")) })

	for _, tc := range []struct {
		name, url string
		dev       bool
		want      string
	}{
		{"stamped", "/assets/app.js?v=abc123", false, "public, max-age=604800, immutable"},
		{"bare", "/assets/app.js", false, "no-cache"},
		{"dev", "/assets/app.js?v=abc123", true, "no-cache"},
	} {
		w := httptest.NewRecorder()
		cacheAssets(inner, tc.dev).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if got := w.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("%s: Cache-Control is %q, want %q", tc.name, got, tc.want)
		}
	}
}
