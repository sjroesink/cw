package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReferenceSchemaAndRoundTrip(t *testing.T) {
	for _, target := range []string{`{"file":"./next.json"}`, `{"url":"https://example.com/w/next"}`, `{"file":"sub/next.json","url":"https://example.com/w/next","stepId":"read"}`} {
		raw := v2doc(`{"type":"reference","title":"Next","description":"More detail","relation":"deep-dive","target":` + target + `}`)
		clean(t, check(t, FormatV2, raw))
		var doc Doc2
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		clean(t, check(t, FormatV2, string(out)))
		if !strings.Contains(string(out), `"description":"More detail"`) || !strings.Contains(string(out), `"relation":"deep-dive"`) {
			t.Fatal(string(out))
		}
	}
	for _, target := range []string{`{}`, `{"file":"../secret.json"}`, `{"file":"./../secret.json"}`, `{"file":"a/../secret.json"}`, `{"file":"a//b.json"}`, `{"file":"/tmp/a.json"}`, `{"file":"C:\\a.json"}`, `{"url":"javascript:alert(1)"}`, `{"stepId":"read"}`} {
		if len(check(t, FormatV2, v2doc(`{"type":"reference","title":"Next","target":`+target+`}`))) == 0 {
			t.Errorf("accepted %s", target)
		}
	}
}

func TestReferenceLocalReader(t *testing.T) {
	s, file := testReader(t)
	target := filepath.Join(filepath.Dir(file), "next.json")
	if err := os.WriteFile(file, []byte(v2doc(`{"type":"reference","title":"Next","target":{"file":"./next.json"}}`)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(v2doc(`{"type":"reference","title":"Return","target":{"file":"wt.json"}}`)), 0600); err != nil {
		t.Fatal(err)
	}
	mux := s.routes()
	post := func(file, token string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"file": file})
		req := httptest.NewRequest("POST", "/api/reference", strings.NewReader(string(body)))
		req.Header.Set("X-Cw-Token", token)
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		return out
	}
	if post("./next.json", "wrong").Code != 403 {
		t.Fatal("unguarded reference")
	}
	if post("unlisted.json", s.token).Code != 400 {
		t.Fatal("unlisted reference accepted")
	}
	response := post("./next.json", s.token)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var link map[string]string
	json.Unmarshal(response.Body.Bytes(), &link)
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest("GET", link["url"], nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "window.CW") {
		t.Fatalf("child page: %d %s", page.Code, page.Body.String())
	}
	// A cycle reuses the initial reader and its comment store rather than
	// creating another nested route and another writer for the same file.
	token := regexp.MustCompile(`token: "([^"]+)"`).FindStringSubmatch(page.Body.String())[1]
	req := httptest.NewRequest("POST", link["url"]+"api/reference", strings.NewReader(`{"file":"wt.json"}`))
	req.Header.Set("X-Cw-Token", token)
	back := httptest.NewRecorder()
	mux.ServeHTTP(back, req)
	if back.Code != 200 || !strings.Contains(back.Body.String(), `"url":"/"`) {
		t.Fatalf("cycle: %d %s", back.Code, back.Body.String())
	}
	if repeated := post("./next.json", s.token); repeated.Body.String() != response.Body.String() {
		t.Fatal("reference route not reused")
	}
	if err := os.WriteFile(target, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if post("./next.json", s.token).Code != 400 {
		t.Fatal("invalid target accepted")
	}
}

func TestReferenceSymlinkContainment(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "private.json")
	os.WriteFile(target, []byte("{}"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip(err)
	}
	if _, err := referenceFile(filepath.Join(root, "tour.json"), "escape/private.json"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
