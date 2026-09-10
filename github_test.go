package main

import (
	"strings"
	"testing"
)

// The anchor GitHub gives a file in a diff is not documented, so this is the
// one place that records what we believe it to be. sha256("doc.go") checked by
// hand against the value GitHub uses.
func TestDiffAnchor(t *testing.T) {
	got := diffAnchor("doc.go")
	want := "diff-a20b1b3b4b2bca5bef2b853a6e3f19def513381f9c7ee68d6979f0435c885dcf"
	if got != want {
		t.Errorf("diffAnchor(doc.go) = %s, want %s", got, want)
	}
	// The path is hashed exactly as it is written, so a leading ./ or a
	// backslash is a different file as far as the anchor is concerned.
	if diffAnchor("src/doc.go") == diffAnchor("doc.go") {
		t.Error("two different paths hashed to the same anchor")
	}
}

func TestPRFilesURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/o/r/pull/3347", "https://github.com/o/r/pull/3347/files"},
		{"https://github.com/o/r/pull/3347/", "https://github.com/o/r/pull/3347/files"},
		{"https://github.com/o/r/pull/3347/files", "https://github.com/o/r/pull/3347/files"},
		{"https://github.com/o/r/pull/3347/commits/abc", "https://github.com/o/r/pull/3347/files"},

		// Not a pull request, so there is nothing to link a diff anchor into.
		{"https://github.com/o/r/commit/abc123", ""},
		{"https://github.com/o/r", ""},
		{"https://dev.azure.com/o/p/_git/r/pullrequest/42", ""},
		{"https://github.com/o/r/pull/not-a-number", ""},
		{"https://github.com/o/r/pull/", ""},
		{"", ""},

		// github.com in somebody's path is not github.com the host, and what
		// comes out of here is what the page opens.
		{"https://example.test/github.com/o/r/pull/3347", ""},
	}
	for _, c := range cases {
		if got := prFilesURL(c.in); got != c.want {
			t.Errorf("prFilesURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildGitHubLinks(t *testing.T) {
	full := &SourceView{
		Provider: "github", Repo: "innovadis-dev/Fincent",
		URL:          "https://github.com/innovadis-dev/Fincent/pull/3347",
		Commit:       "9f2c1ab",
		ChangedFiles: []string{"doc.go"},
	}
	g := BuildGitHubLinks(full)
	if g == nil {
		t.Fatal("a pull request with a commit produced no links at all")
	}
	if g.BlobBase != "https://github.com/innovadis-dev/Fincent/blob/9f2c1ab/" {
		t.Errorf("blob base is %q", g.BlobBase)
	}
	if _, ok := g.Anchors["doc.go"]; !ok {
		t.Error("a changed file got no diff anchor")
	}
	if _, ok := g.Anchors["main.go"]; ok {
		t.Error("a file that is not in changedFiles got an anchor")
	}

	// A commit and no pull request still permalinks, which is the subsystem case.
	if g := BuildGitHubLinks(&SourceView{Repo: "o/r", Commit: "abc1234"}); g == nil || g.BlobBase == "" || g.PRFiles != "" {
		t.Errorf("a bare commit should permalink and nothing else, got %+v", g)
	}

	// Nothing to point at is nil rather than an object full of empty strings,
	// so the page has one case to check instead of four.
	for _, src := range []*SourceView{
		nil,
		{},
		{Repo: "o/r"},
		{Provider: "azure-devops", Repo: "o/r", Commit: "abc1234"},
	} {
		if g := BuildGitHubLinks(src); g != nil {
			t.Errorf("BuildGitHubLinks(%+v) = %+v, want nil", src, g)
		}
	}
}

func TestRepoFromRemoteReadsBothShapesGitWrites(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:innovadis-dev/Fincent.git":     "innovadis-dev/Fincent",
		"https://github.com/innovadis-dev/Fincent.git": "innovadis-dev/Fincent",
		"https://github.com/cli/cli\n":                 "cli/cli",
		"ssh://git@github.com/cli/cli.git":             "cli/cli",
		"":                                             "",
		"/some/local/path":                             "",
	} {
		if got := repoFromRemote(remote); got != want {
			t.Errorf("repoFromRemote(%q) = %q, want %q", remote, got, want)
		}
	}
}

// Nothing to ask about is not the same as an answer, and it must not read as
// one: a walkthrough with no repository in it goes out locked.
func TestRepoIsPublicSaysNoWhenItCannotKnow(t *testing.T) {
	public, why := RepoIsPublic("", "")
	if public {
		t.Errorf("called a repository it cannot name public")
	}
	if !strings.Contains(why, "names no repository") {
		t.Errorf("said %q", why)
	}
}
