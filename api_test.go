package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testHost(t *testing.T) (*hostServer, string) {
	t.Helper()
	store := testStore(t)
	key, err := store.AddKey("test")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	return &hostServer{store: store, schema: mustSchema(), vendor: NewVendor(true),
		web: sub, base: "https://cw.example"}, key
}

func do(t *testing.T, h *hostServer, method, path, key, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.routes().ServeHTTP(rec, req)

	var out map[string]any
	if bytes.HasPrefix(bytes.TrimSpace(rec.Body.Bytes()), []byte("{")) {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s answered with something that is not JSON: %v", method, path, err)
		}
	}
	return rec, out
}

const goodDoc = `{
  "version": "cw/1",
  "title": "A change worth reading",
  "summary": "What it does and why.",
  "source": {"kind":"pull-request","provider":"github","repo":"o/r","number":"PR #7",
             "url":"https://github.com/o/r/pull/7","commit":"abc1234","changedFiles":["a.go"]},
  "parts": [{"title":"The fix","desc":"One line.","sections":[{"title":"How","steps":[
    {"title":"It reads the file","body":"And then it stops.",
     "code":{"file":"a.go","from":3,"text":"package main\n\nfunc main() {}"}}]}]}]
}`

func TestPublishAndRead(t *testing.T) {
	h, _ := testHost(t)

	rec, out := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish returned %d: %s", rec.Code, rec.Body)
	}
	slug, _ := out["slug"].(string)
	if slug != "r-pr-7" {
		t.Errorf("the name was derived as %q, want r-pr-7", slug)
	}
	if out["url"] != "https://cw.example/w/r-pr-7" {
		t.Errorf("the URL came back as %v", out["url"])
	}

	// The page reads this, and it has to carry everything the page needs to
	// turn a line number into a link without asking a second time.
	rec, out = do(t, h, "GET", "/api/v1/walkthroughs/"+slug, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reading it back returned %d", rec.Code)
	}
	if out["hosted"] != true {
		t.Error("the payload does not say it is hosted, so the page will try to open an editor")
	}
	gh, _ := out["github"].(map[string]any)
	if gh == nil || gh["prFiles"] != "https://github.com/o/r/pull/7/files" {
		t.Errorf("the github block is %v", out["github"])
	}

	// Ids and snippet anchors are filled in on publish, so an author never has
	// to type them and a consumer can always rely on them.
	doc, _ := out["doc"].(map[string]any)
	parts, _ := doc["parts"].([]any)
	part, _ := parts[0].(map[string]any)
	if part["id"] != "the-fix" {
		t.Errorf("the part id was filled in as %v, want the-fix", part["id"])
	}
	step := part["sections"].([]any)[0].(map[string]any)["steps"].([]any)[0].(map[string]any)
	code := step["code"].(map[string]any)
	if code["to"] != float64(5) {
		t.Errorf("code.to was filled in as %v, want 5", code["to"])
	}
	if s, _ := code["sha"].(string); len(s) != 64 {
		t.Errorf("code.sha was filled in as %q", s)
	}

	// And reading it needs no key, because a walkthrough is meant to be shared.
	if rec, _ := do(t, h, "GET", "/api/v1/walkthroughs/"+slug, "", ""); rec.Code != http.StatusOK {
		t.Errorf("reading without a key returned %d", rec.Code)
	}
}

// Publishing is open, and what comes back is the key to that one walkthrough.
func TestPublishingIsOpenAndHandsBackAKey(t *testing.T) {
	h, _ := testHost(t)

	rec, out := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	if rec.Code != http.StatusOK {
		t.Fatalf("publishing without a key returned %d: %s", rec.Code, rec.Body)
	}
	key, _ := out["key"].(string)
	if !strings.HasPrefix(key, "cwp_") || len(key) < 20 {
		t.Fatalf("the key that came back is %q", key)
	}

	// It is handed over once. Updating does not mint a second one.
	_, again := do(t, h, "PUT", "/api/v1/walkthroughs/r-pr-7", key, goodDoc)
	if _, handed := again["key"]; handed {
		t.Error("updating handed out another key")
	}

	// And it is stored as a hash, nowhere near what a reader can see.
	rec, payload := do(t, h, "GET", "/api/v1/walkthroughs/r-pr-7", "", "")
	if strings.Contains(rec.Body.String(), key) {
		t.Error("the key came back in the payload the page reads")
	}
	if meta, _ := payload["meta"].(map[string]any); meta != nil {
		for field := range meta {
			if strings.Contains(strings.ToLower(field), "key") {
				t.Errorf("meta carries a %q field, which is served to every reader", field)
			}
		}
	}
}

// The key belongs to one walkthrough, so it does not open the one next to it.
func TestAKeyOnlyOpensItsOwnWalkthrough(t *testing.T) {
	h, admin := testHost(t)
	_, mine := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	_, theirs := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)

	mineKey, theirsSlug := mine["key"].(string), theirs["slug"].(string)
	if rec, _ := do(t, h, "PUT", "/api/v1/walkthroughs/"+theirsSlug, mineKey, goodDoc); rec.Code != http.StatusUnauthorized {
		t.Errorf("one walkthrough's key changed another one: %d", rec.Code)
	}
	if rec, _ := do(t, h, "DELETE", "/api/v1/walkthroughs/"+theirsSlug, mineKey, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("one walkthrough's key deleted another one: %d", rec.Code)
	}

	// An admin key works on everything, which is how a lost key is recovered
	// and how something that should not be there is removed.
	if rec, _ := do(t, h, "PUT", "/api/v1/walkthroughs/"+theirsSlug, admin, goodDoc); rec.Code != http.StatusOK {
		t.Errorf("the admin key could not change a walkthrough it did not publish: %d", rec.Code)
	}
	if rec, _ := do(t, h, "DELETE", "/api/v1/walkthroughs/"+theirsSlug, admin, ""); rec.Code != http.StatusOK {
		t.Errorf("the admin key could not delete: %d", rec.Code)
	}
}

func TestPublishRefusesABrokenDocument(t *testing.T) {
	h, _ := testHost(t)
	// hi points outside the snippet, which the schema cannot catch and
	// inspect() does.
	broken := strings.Replace(goodDoc,
		`"code":{"file":"a.go","from":3,`,
		`"code":{"file":"a.go","from":3,"hi":[99],`, 1)

	rec, out := do(t, h, "POST", "/api/v1/walkthroughs", "", broken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a broken document returned %d, want 400", rec.Code)
	}
	errs, _ := out["errors"].([]any)
	if len(errs) == 0 {
		t.Fatal("400 with no explanation of what is wrong")
	}
	if !strings.Contains(errs[0].(string), "line 99") {
		t.Errorf("the error does not say what is wrong: %v", errs[0])
	}
	if list, _ := h.store.List(); len(list) != 0 {
		t.Error("a document that failed validation was stored anyway")
	}
}

func TestUpdateKeepsTheURLAndTheCreationDate(t *testing.T) {
	h, _ := testHost(t)
	_, made := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	key, _ := made["key"].(string)
	_, first, _ := h.store.Get("r-pr-7")

	updated := strings.Replace(goodDoc, "A change worth reading", "A better title", 1)
	rec, _ := do(t, h, "PUT", "/api/v1/walkthroughs/r-pr-7", key, updated)
	if rec.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", rec.Code, rec.Body)
	}

	doc, meta, err := h.store.Get("r-pr-7")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "A better title" {
		t.Errorf("the update did not land: %q", doc.Title)
	}
	if !meta.CreatedAt.Equal(first.CreatedAt) {
		t.Error("updating in place reset the creation date")
	}
	if !meta.UpdatedAt.After(first.UpdatedAt) && !meta.UpdatedAt.Equal(first.UpdatedAt) {
		t.Error("the update stamp went backwards")
	}
	if list, _ := h.store.List(); len(list) != 1 {
		t.Errorf("updating made a second walkthrough: %d", len(list))
	}
}

// Publishing twice without saying where is two walkthroughs, not one
// overwriting the other. Losing somebody else's page to a name collision is
// worse than an ugly slug.
func TestPublishingTwiceDoesNotOverwrite(t *testing.T) {
	h, _ := testHost(t)
	_, first := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	_, second := do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)
	if first["slug"] == second["slug"] {
		t.Fatalf("both publishes claimed %v", first["slug"])
	}
	if second["slug"] != "r-pr-7-2" {
		t.Errorf("the second publish was named %v, want r-pr-7-2", second["slug"])
	}

	// Asking for a name that is taken is refused rather than counted up,
	// because the caller said which one they meant.
	rec, _ := do(t, h, "POST", "/api/v1/walkthroughs", "",
		`{"slug":"r-pr-7","walkthrough":`+goodDoc+`}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("asking for a taken name returned %d, want 409", rec.Code)
	}
}

func TestEnvelopeChoosesTheName(t *testing.T) {
	h, _ := testHost(t)
	rec, out := do(t, h, "POST", "/api/v1/walkthroughs", "",
		`{"slug":"my-own-name","walkthrough":`+goodDoc+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish with an envelope returned %d: %s", rec.Code, rec.Body)
	}
	if out["slug"] != "my-own-name" {
		t.Errorf("the name was %v, want my-own-name", out["slug"])
	}

	rec, _ = do(t, h, "POST", "/api/v1/walkthroughs", "",
		`{"slug":"Not A Slug","walkthrough":`+goodDoc+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an impossible name returned %d, want 400", rec.Code)
	}
}

// Only changing what is already there needs a key. Reading and publishing do not.
func TestOnlyChangingNeedsAKey(t *testing.T) {
	h, _ := testHost(t)
	do(t, h, "POST", "/api/v1/walkthroughs", "", goodDoc)

	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/v1/walkthroughs/r-pr-7"},
		{"DELETE", "/api/v1/walkthroughs/r-pr-7"},
	} {
		if rec, _ := do(t, h, c.method, c.path, "", goodDoc); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a key returned %d, want 401", c.method, c.path, rec.Code)
		}
		if rec, _ := do(t, h, c.method, c.path, "cwp_wrong", goodDoc); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a wrong key returned %d, want 401", c.method, c.path, rec.Code)
		}
	}

	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/walkthroughs"},
		{"POST", "/api/v1/validate"},
	} {
		if rec, _ := do(t, h, c.method, c.path, "", goodDoc); rec.Code != http.StatusOK {
			t.Errorf("%s %s without a key returned %d, want 200", c.method, c.path, rec.Code)
		}
	}

	// Reading is open all the way down, including the index: the landing page
	// shows it to anyone anyway, and every walkthrough on it is readable by URL.
	for _, path := range []string{"/api/v1/walkthroughs", "/api/v1/walkthroughs/r-pr-7",
		"/api/v1/walkthroughs/r-pr-7/state", "/schema/v1.json", "/llms.txt", "/skill.md",
		"/format", "/w/r-pr-7", "/"} {
		if rec, _ := do(t, h, "GET", path, "", ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s without a key returned %d, want 200", path, rec.Code)
		}
	}
}

func TestValidateStoresNothing(t *testing.T) {
	h, _ := testHost(t)
	rec, out := do(t, h, "POST", "/api/v1/validate", "", goodDoc)
	if rec.Code != http.StatusOK || out["ok"] != true {
		t.Fatalf("validating a good document returned %d, %v", rec.Code, out)
	}
	if list, _ := h.store.List(); len(list) != 0 {
		t.Error("validate stored the document")
	}
}

// The instructions an agent arrives for have to name the site it is talking to,
// not the site the binary was built on.
func TestAgentDocsCarryTheBaseURL(t *testing.T) {
	h, _ := testHost(t)
	for _, path := range []string{"/llms.txt", "/skill.md"} {
		rec, _ := do(t, h, "GET", path, "", "")
		body := rec.Body.String()
		if !strings.Contains(body, "https://cw.example") {
			t.Errorf("%s does not mention the base URL", path)
		}
		if strings.Contains(body, "__BASE__") {
			t.Errorf("%s still has an unfilled placeholder in it", path)
		}
	}
	rec, _ := do(t, h, "GET", "/skill.md", "", "")
	for _, must := range []string{"Writing a walkthrough", "The API", "walkthrough file"} {
		if !strings.Contains(rec.Body.String(), must) {
			t.Errorf("/skill.md is missing the %q section, so an agent cannot do the job from it", must)
		}
	}
}

func TestUnknownWalkthrough(t *testing.T) {
	h, key := testHost(t)
	for _, c := range []struct {
		method, path, key string
		want              int
	}{
		{"GET", "/api/v1/walkthroughs/nothing", "", http.StatusNotFound},
		{"GET", "/api/v1/walkthroughs/nothing/state", "", http.StatusNotFound},
		{"DELETE", "/api/v1/walkthroughs/nothing", key, http.StatusNotFound},
		{"PUT", "/api/v1/walkthroughs/nothing", key, http.StatusNotFound},
		{"GET", "/w/nothing", "", http.StatusNotFound},
	} {
		if rec, _ := do(t, h, c.method, c.path, c.key, ""); rec.Code != c.want {
			t.Errorf("%s %s returned %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}
