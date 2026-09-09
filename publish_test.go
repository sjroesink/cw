package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Everything derivable from the pull request URL alone, which is what a machine
// without gh, or without access to the repository, still gets.
func TestFillSourceFromTheURL(t *testing.T) {
	d := &Doc{Source: &Source{URL: "https://github.com/innovadis-dev/Fincent/pull/3347"}}
	fillSource(d, "")

	s := d.Source
	if s.Repo != "innovadis-dev/Fincent" {
		t.Errorf("repo was derived as %q", s.Repo)
	}
	if s.Provider != "github" || s.Kind != "pull-request" {
		t.Errorf("provider/kind were derived as %q %q", s.Provider, s.Kind)
	}
}

// What the author wrote wins, because they may know something the URL does not.
func TestFillSourceLeavesWhatIsThere(t *testing.T) {
	d := &Doc{Source: &Source{
		URL:      "https://github.com/o/r/pull/1",
		Repo:     "somewhere/else",
		Kind:     "subsystem",
		Provider: "gitlab",
		Commit:   "1234567",
	}}
	fillSource(d, "")

	s := d.Source
	if s.Repo != "somewhere/else" || s.Kind != "subsystem" || s.Provider != "gitlab" || s.Commit != "1234567" {
		t.Errorf("fillSource overwrote what the author wrote: %+v", s)
	}
}

// A walkthrough of a subsystem has no pull request, and must still end up with
// something to build a permalink from.
func TestFillSourceWithoutAPullRequest(t *testing.T) {
	root := t.TempDir()
	if _, err := runIn(root, "git", "init", "-q"); err != nil {
		t.Skip("git is not available")
	}
	write(t, filepath.Join(root, "a.txt"), "hello")
	mustRun(t, root, "git", "-c", "user.email=t@example", "-c", "user.name=t", "add", "a.txt")
	mustRun(t, root, "git", "-c", "user.email=t@example", "-c", "user.name=t", "commit", "-q", "-m", "one")

	d := &Doc{Source: &Source{Kind: "subsystem"}}
	fillSource(d, root)
	if len(d.Source.Commit) != 40 {
		t.Errorf("the commit was read as %q, want a full object name", d.Source.Commit)
	}
}

func TestFillSourceMakesOneWhenThereIsNone(t *testing.T) {
	d := &Doc{}
	fillSource(d, "")
	if d.Source == nil {
		t.Fatal("a walkthrough with no source at all came back with none")
	}
}

// The note beside the file is what makes a second publish an update rather than
// a second walkthrough at a URL nobody has.
func TestTheNoteDecidesUpdateOrNew(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "walkthrough.json")
	write(t, file, "{}")

	f := publishFlags{file: file, site: "https://cw.example"}
	if url, method := f.resolveTarget(); method != "POST" || url != "https://cw.example/api/v1/walkthroughs" {
		t.Errorf("a file that was never published went to %s %s", method, url)
	}

	rememberPublish(file, published{Site: "https://cw.example", Slug: "already-there", At: time.Now()})
	url, method := f.resolveTarget()
	if method != "PUT" || url != "https://cw.example/api/v1/walkthroughs/already-there" {
		t.Errorf("a file that was published before went to %s %s, want a PUT in place", method, url)
	}

	// --new is how you say you meant a second one.
	f.isNew = true
	if _, method := f.resolveTarget(); method != "POST" {
		t.Errorf("--new used %s", method)
	}

	// And the note belongs to one site: publishing the same file somewhere else
	// must not update a name that only exists on the first one.
	f.isNew = false
	f.site = "https://other.example"
	if _, method := f.resolveTarget(); method != "POST" {
		t.Errorf("publishing to a different site used %s, want POST", method)
	}
}

func TestAPIURLTakesWhateverWasPasted(t *testing.T) {
	site := "https://cw.example"
	cases := map[string]string{
		"fincent-pr-3347":                                        "https://cw.example/api/v1/walkthroughs/fincent-pr-3347",
		"https://cw.example/w/fincent-pr-3347":                   "https://cw.example/api/v1/walkthroughs/fincent-pr-3347",
		"https://cw.example/w/fincent-pr-3347/":                  "https://cw.example/api/v1/walkthroughs/fincent-pr-3347",
		"https://cw.example/api/v1/walkthroughs/fincent-pr-3347": "https://cw.example/api/v1/walkthroughs/fincent-pr-3347",
		"http://127.0.0.1:8080/w/x":                              "http://127.0.0.1:8080/api/v1/walkthroughs/x",
	}
	for in, want := range cases {
		if got := apiURL(in, site); got != want {
			t.Errorf("apiURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	if out, err := runIn(dir, name, args...); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
