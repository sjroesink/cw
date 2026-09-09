package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// A file written before the format had a name. The four header fields at the
// top level are what cw/1 replaced with source.
const legacyDoc = `{
  "title": "An older walkthrough",
  "repo": "o/r",
  "number": "PR #7",
  "state": "merged",
  "url": "https://github.com/o/r/pull/7",
  "summary": "Still readable.",
  "parts": [{"title":"One","desc":"x","sections":[{"title":"Two","steps":[
    {"title":"Three","body":"Four"}]}]}]
}`

func TestLegacyFilesStillLoad(t *testing.T) {
	res, err := ParseDoc([]byte(legacyDoc), "legacy.json", mustSchema())
	if err != nil {
		t.Fatalf("an older file failed to load: %v", err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("an older file came back with errors: %v", res.Errors)
	}
	if res.Doc.Version != FormatVersion {
		t.Errorf("version is %q after loading, want %q", res.Doc.Version, FormatVersion)
	}
	if res.Doc.Source == nil {
		t.Fatal("the header fields did not become a source block")
	}
	src := res.Doc.Source
	if src.Repo != "o/r" || src.Number != "PR #7" || src.State != "merged" {
		t.Errorf("source came out as %+v", src)
	}
	// The provider and the kind are worked out from the URL, because they are
	// knowable and an author should not have to add them by hand.
	if src.Provider != "github" || src.Kind != "pull-request" {
		t.Errorf("provider/kind were not derived: %q %q", src.Provider, src.Kind)
	}

	// And the reader is told, once, that the file on disk is still the old shape.
	if !anyContains(res.Warnings, "cw migrate") {
		t.Errorf("loading an old file said nothing about migrating: %v", res.Warnings)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(legacyDoc), &obj); err != nil {
		t.Fatal(err)
	}
	if !migrateLegacy(obj) {
		t.Fatal("the first migration reported no change")
	}
	if migrateLegacy(obj) {
		t.Error("migrating an already-migrated document changed it again")
	}
	for _, gone := range []string{"repo", "number", "state", "url"} {
		if _, still := obj[gone]; still {
			t.Errorf("%q is still at the top level after migrating", gone)
		}
	}
}

func TestEnsureIDsLeavesExistingOnesAlone(t *testing.T) {
	d := &Doc{Version: FormatVersion, Title: "x", Parts: []Part{
		{ID: "kept", Title: "Some part", Sections: []Section{
			{Title: "A section", Steps: []Step{
				{Title: "The same title"}, {Title: "The same title"}}}}},
	}}
	EnsureIDs(d)

	if d.Parts[0].ID != "kept" {
		t.Errorf("an id that was already there was rewritten to %q", d.Parts[0].ID)
	}
	if d.Parts[0].Sections[0].ID != "a-section" {
		t.Errorf("section id is %q", d.Parts[0].Sections[0].ID)
	}
	// Two steps with the same title must not end up with the same id, or a
	// link to one of them lands on the other.
	steps := d.Parts[0].Sections[0].Steps
	if steps[0].ID == steps[1].ID {
		t.Fatalf("both steps got the id %q", steps[0].ID)
	}
	if steps[0].ID != "the-same-title" || steps[1].ID != "the-same-title-2" {
		t.Errorf("step ids are %q and %q", steps[0].ID, steps[1].ID)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"The comparison is timing-safe": "the-comparison-is-timing-safe",
		"HMAC verification middleware":  "hmac-verification-middleware",
		"src/http/verify.ts":            "src-http-verify-ts",
		"  spaced  out  ":               "spaced-out",
		"!!!":                           "x",
		"":                              "x",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slug(strings.Repeat("long ", 40)); len(got) > 48 {
		t.Errorf("slug did not cap the length: %d characters", len(got))
	}
}

func TestEnsureAnchorsAndTheChecksOnThem(t *testing.T) {
	d := &Doc{Version: FormatVersion, Title: "x", Parts: []Part{{Title: "p",
		Sections: []Section{{Title: "s", Steps: []Step{
			{Title: "t", Body: "b", Code: &Code{File: "a.go", From: 10, Text: "one\ntwo\nthree"}},
			// No from, so it is a shape rather than a place in a file: it gets
			// a hash and no line range.
			{Title: "t2", Body: "b", Code: &Code{File: "b.go", Text: "shape"}},
		}}}}}}
	EnsureAnchors(d)

	first := d.Parts[0].Sections[0].Steps[0].Code
	if first.To != 12 {
		t.Errorf("to was filled in as %d, want 12", first.To)
	}
	if len(first.Sha) != 64 {
		t.Errorf("sha was filled in as %q", first.Sha)
	}
	second := d.Parts[0].Sections[0].Steps[1].Code
	if second.To != 0 {
		t.Errorf("a snippet with no from was given a to of %d", second.To)
	}

	// The hash folds line endings and trailing blank lines, so the same code
	// hashes the same on a Windows machine and on a Linux one.
	if snippetSha("one\r\ntwo\r\nthree\r\n") != snippetSha("one\ntwo\nthree") {
		t.Error("the snippet hash depends on line endings")
	}

	// An anchor that no longer matches its text is an error, because a
	// consumer would act on it.
	var errs []string
	errf := func(f string, a ...any) { errs = append(errs, f) }
	anchorErrs(errf, "at", 10, 12, first.Sha, "one\ntwo\nthree\nfour")
	if len(errs) != 2 {
		t.Errorf("editing the text under its anchor produced %d errors, want 2 (to and sha)", len(errs))
	}
}

func anyContains(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
