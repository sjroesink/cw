package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameCommitComparesEitherWayRound(t *testing.T) {
	full := "144da9aa073bdb720c1b9a271504b0e9ec4ea627"
	for _, other := range []string{full, "144da9a", "144da9aa073bdb720c1b9a271504b0e9ec4ea627"} {
		if !sameCommit(full, other) || !sameCommit(other, full) {
			t.Errorf("%q and %q did not compare equal", full, other)
		}
	}
	if sameCommit(full, "68fa333") || sameCommit("", full) || sameCommit(full, "") {
		t.Error("two different commits compared equal")
	}
}

// A walkthrough without a commit says nothing about where it should be read,
// and neither does a forge that has nothing to add, so the checkout that was
// found stays and nothing is offered.
func TestCheckoutForLeavesThingsAloneWithoutACommit(t *testing.T) {
	for _, d := range []*SourceView{nil, {}} {
		p := checkoutFor("C:/Projects/Fincent", false, d, nil)
		if p.Root != "C:/Projects/Fincent" || p.Notes != nil || p.Add != nil || p.Move != nil {
			t.Errorf("checkoutFor moved to %q and said %v", p.Root, p.Notes)
		}
	}
	if p := checkoutFor("", false, &SourceView{Commit: "abc"}, nil); p.Root != "" || p.Notes != nil {
		t.Errorf("checkoutFor without a checkout returned %q and %v", p.Root, p.Notes)
	}
	quiet := func() branchInfo { return branchInfo{Note: "gh is not on this machine"} }
	if p := checkoutFor("C:/Projects/Fincent", false, &SourceView{}, quiet); p.Notes != nil || p.Add != nil {
		t.Errorf("a branch nobody could name still produced %v", p.Notes)
	}
}

// The whole of it against a real repository: two commits, a worktree left on
// the first, and a walkthrough written against that first commit. Reading it
// has to land in the worktree rather than tell anybody to move a branch.
func TestCheckoutForFindsTheWorktreeOnTheCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(where string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = where
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(main, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(main, "init", "-b", "main")
	write("a.txt", "one\n")
	git(main, "add", ".")
	git(main, "commit", "-m", "one")
	first := git(main, "rev-parse", "HEAD")
	write("a.txt", "two\n")
	git(main, "commit", "-am", "two")

	d := &SourceView{Commit: first, URL: "https://github.com/x/y/pull/7"}

	// With nothing checked out on it one is offered, the checkout stays where
	// it is, and what gh said about the branch is repeated.
	p := checkoutFor(main, false, d, func() branchInfo {
		return branchInfo{Name: "feature/x", Note: "pull request 7 is on branch feature/x"}
	})
	if p.Root != main {
		t.Errorf("moved to %q with no worktree to move to", p.Root)
	}
	want := p.Add
	if want == nil {
		t.Fatalf("offered no worktree, and said only:\n%s", strings.Join(p.Notes, "\n"))
	}
	if want.Commit != first || want.Branch != "feature/x" || len(want.Fetch) != 0 {
		t.Errorf("offered %+v, and that commit is right here", want)
	}
	joined := strings.Join(append(p.Notes, offerNotes(want, d)...), "\n")
	if !strings.Contains(joined, "git worktree add --detach") {
		t.Errorf("did not say how to make one by hand:\n%s", joined)
	}
	if !strings.Contains(joined, "pull request 7 is on branch feature/x") {
		t.Errorf("kept what gh said to itself:\n%s", joined)
	}
	if strings.Contains(joined, "not here yet") {
		t.Errorf("asked for a fetch of a commit that is right there:\n%s", joined)
	}

	// Now there is one, so that is where it reads.
	side := filepath.Join(dir, "on-the-commit")
	git(main, "worktree", "add", "--detach", side, first)
	p = checkoutFor(main, false, d, nil)
	if !samePath(p.Root, side) {
		t.Fatalf("read against %q, want the worktree at %q", p.Root, side)
	}
	if p.Add != nil || p.Move != nil {
		t.Errorf("asked for something with a worktree already on the commit: %+v %+v", p.Add, p.Move)
	}
	joined = strings.Join(p.Notes, "\n")
	if !strings.Contains(joined, "so that is what will be read") {
		t.Errorf("did not say what it did:\n%s", joined)
	}

	// Unless the reader named a root themselves, which is not overruled.
	p = checkoutFor(main, true, d, nil)
	if p.Root != main {
		t.Errorf("moved away from the root that was asked for, to %q", p.Root)
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "would line up better") {
		t.Errorf("did not mention the worktree:\n%s", strings.Join(p.Notes, "\n"))
	}
}
