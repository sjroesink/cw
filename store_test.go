package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func sampleDoc(title string) *Doc {
	return &Doc{
		Version: FormatV1, Title: title,
		Parts: []Part{{Title: "One", Sections: []Section{{Title: "Two",
			Steps: []Step{{Title: "Three", Body: "Four"}}}}}},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := testStore(t)
	if s.Exists("nothing") {
		t.Error("an empty store claims to hold something")
	}
	if _, _, err := s.Get("nothing"); err != ErrNoSuchWalkthrough {
		t.Errorf("Get on an empty store returned %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	want := sampleDoc("A walkthrough")
	if err := s.Put("thing", want, Meta{Slug: "thing", Title: want.Title, Steps: 1, UpdatedAt: now}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, meta, err := s.Get("thing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != want.Title || len(got.Parts) != 1 {
		t.Errorf("the document came back different: %+v", got)
	}
	if meta.Slug != "thing" || !meta.UpdatedAt.Equal(now) {
		t.Errorf("the metadata came back different: %+v", meta)
	}

	if err := s.Delete("thing"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Exists("thing") {
		t.Error("it is still there after being deleted")
	}
	if err := s.Delete("thing"); err != ErrNoSuchWalkthrough {
		t.Errorf("deleting it twice returned %v", err)
	}
}

// A slug becomes a directory name and the tail of a URL, so it is checked
// rather than cleaned: anything that could climb out of the data directory is
// refused outright.
func TestSlugsAreRefusedNotCleaned(t *testing.T) {
	s := testStore(t)
	for _, bad := range []string{"", "..", "../escape", "a/b", `a\b`, "Caps", "with space", "trailing-",
		"x0123456789012345678901234567890123456789012345678901234567890123456789"} {
		if ValidSlug(bad) {
			t.Errorf("%q was accepted as a name", bad)
		}
		if err := s.Put(bad, sampleDoc("x"), Meta{}); err == nil {
			t.Errorf("Put(%q) was allowed", bad)
		}
	}
	for _, ok := range []string{"a", "fincent-pr-3347", "0", "a-b-c"} {
		if !ValidSlug(ok) {
			t.Errorf("%q was refused as a name", ok)
		}
	}
}

func TestListIsNewestFirst(t *testing.T) {
	s := testStore(t)
	base := time.Now().UTC()
	for i, slug := range []string{"oldest", "middle", "newest"} {
		m := Meta{Slug: slug, Title: slug, UpdatedAt: base.Add(time.Duration(i) * time.Hour)}
		if err := s.Put(slug, sampleDoc(slug), m); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with no readable metadata is skipped rather than failing the
	// whole listing.
	if err := os.MkdirAll(filepath.Join(s.Dir, "walkthroughs", "broken"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List returned %d entries, want 3", len(list))
	}
	if list[0].Slug != "newest" || list[2].Slug != "oldest" {
		t.Errorf("List is not newest first: %v", []string{list[0].Slug, list[1].Slug, list[2].Slug})
	}
}

func TestFreeSlugCountsUp(t *testing.T) {
	s := testStore(t)
	if got := s.FreeSlug("taken"); got != "taken" {
		t.Errorf("an unused name should come back as it is, got %q", got)
	}
	_ = s.Put("taken", sampleDoc("x"), Meta{})
	if got := s.FreeSlug("taken"); got != "taken-2" {
		t.Errorf("FreeSlug after one collision = %q, want taken-2", got)
	}
	_ = s.Put("taken-2", sampleDoc("x"), Meta{})
	if got := s.FreeSlug("taken"); got != "taken-3" {
		t.Errorf("FreeSlug after two collisions = %q, want taken-3", got)
	}
}

// A half-written document must never be readable, and a write that fails must
// leave the previous version standing.
func TestWritesAreAtomic(t *testing.T) {
	s := testStore(t)
	if err := s.Put("thing", sampleDoc("first"), Meta{Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("thing", sampleDoc("second"), Meta{Title: "second"}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Get("thing")
	if err != nil || got.Title != "second" {
		t.Fatalf("after a second write: %v, %+v", err, got)
	}
	// Nothing is left lying around beside the two files that belong there.
	entries, _ := os.ReadDir(s.dirFor("thing"))
	if len(entries) != 2 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the walkthrough directory holds %v, want just doc.json and meta.json", names)
	}
}

func TestKeys(t *testing.T) {
	s := testStore(t)
	if name, ok := s.MatchKey("cw_nothing"); ok {
		t.Errorf("an empty store matched a key as %q", name)
	}

	key, err := s.AddKey("laptop")
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	name, ok := s.MatchKey(key)
	if !ok || name != "laptop" {
		t.Errorf("MatchKey on the key just minted = %q, %v", name, ok)
	}
	if _, ok := s.MatchKey(key + "x"); ok {
		t.Error("a wrong key matched")
	}
	if _, ok := s.MatchKey(""); ok {
		t.Error("an empty key matched")
	}

	// The key itself is never written down, only its hash.
	raw, err := os.ReadFile(s.keysPath())
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(raw), key) {
		t.Error("the key file contains the key in the clear")
	}

	if n, err := s.RemoveKey("laptop"); err != nil || n != 1 {
		t.Fatalf("RemoveKey = %d, %v", n, err)
	}
	if _, ok := s.MatchKey(key); ok {
		t.Error("a removed key still matches")
	}
}

func TestBootstrapKeyIsStoredOnce(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 3; i++ {
		if err := s.AddKeyValue("bootstrap", "cw_deadbeef"); err != nil {
			t.Fatal(err)
		}
	}
	keys, _ := s.Keys()
	if len(keys) != 1 {
		t.Errorf("restarting with the same bootstrap key added %d keys", len(keys))
	}
	if name, ok := s.MatchKey("cw_deadbeef"); !ok || name != "bootstrap" {
		t.Errorf("the bootstrap key does not authenticate: %q %v", name, ok)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
