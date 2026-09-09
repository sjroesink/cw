package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The porcelain form as git prints it, with a detached worktree and a bare
// clone in it, because both are shapes that have no branch to match on.
const worktreeList = `worktree C:/Projects/Fincent
HEAD 68fa333fabff85b525288338c941c54eade957c3
branch refs/heads/feature/FIN-7899-wallboard-pool-panel

worktree C:/Projects/Fincent/.claude/worktrees/maintenance-per-env
HEAD c0085c82f84693e75288fc564c951907e1520f63
branch refs/heads/worktree-maintenance-per-env

worktree C:/Users/somebody/.claude-worktrees/Fincent/pr-4164
HEAD 144da9aa073bdb720c1b9a271504b0e9ec4ea627
detached

worktree C:/Projects/Fincent-mirror
bare
`

func TestParseWorktrees(t *testing.T) {
	all := parseWorktrees(worktreeList)
	if len(all) != 4 {
		t.Fatalf("parsed %d worktrees, want 4", len(all))
	}
	if all[0].Branch != "feature/FIN-7899-wallboard-pool-panel" {
		t.Errorf("the branch came out as %q", all[0].Branch)
	}
	if all[2].Branch != "" || all[2].Head != "144da9aa073bdb720c1b9a271504b0e9ec4ea627" {
		t.Errorf("a detached worktree came out as %+v", all[2])
	}
	if !all[3].Bare {
		t.Errorf("the bare clone did not come out bare: %+v", all[3])
	}
	if all[0].Path != filepath.Clean("C:/Projects/Fincent") {
		t.Errorf("the path came out as %q", all[0].Path)
	}
}

// Being on exactly the commit beats being on the branch, because a branch that
// has moved on since is how the snippets drift in the first place.
func TestPickWorktreePrefersTheCommitOverTheBranch(t *testing.T) {
	all := parseWorktrees(worktreeList)
	root := "C:/Projects/Fincent"

	w, why := pickWorktree(all, root, "144da9aa073bdb720c1b9a271504b0e9ec4ea627", "worktree-maintenance-per-env")
	if w.Path != filepath.Clean("C:/Users/somebody/.claude-worktrees/Fincent/pr-4164") {
		t.Fatalf("picked %q, want the one on the commit", w.Path)
	}
	if why != "on 144da9a" {
		t.Errorf("said %q", why)
	}

	// No worktree on that commit, so the branch is the next best thing.
	w, why = pickWorktree(all, root, "0000000000000000000000000000000000000000", "worktree-maintenance-per-env")
	if w.Path != filepath.Clean("C:/Projects/Fincent/.claude/worktrees/maintenance-per-env") {
		t.Fatalf("picked %q, want the one on the branch", w.Path)
	}
	if why != "on worktree-maintenance-per-env" {
		t.Errorf("said %q", why)
	}

	// Neither, so nothing. Suggesting a checkout that is on neither the commit
	// nor the branch would be worse than saying there is none.
	if w, _ := pickWorktree(all, root, "0000000", "no-such-branch"); w.Path != "" {
		t.Errorf("picked %q with nothing to match on", w.Path)
	}
}

// The checkout being read already is never the answer to being on the wrong
// commit, and a bare clone has no files to read at all.
func TestPickWorktreeSkipsTheCheckoutItselfAndBareOnes(t *testing.T) {
	all := parseWorktrees(worktreeList)
	if w, _ := pickWorktree(all, "C:/Projects/Fincent", "68fa333", ""); w.Path != "" {
		t.Errorf("picked %q, which is the checkout it was called about", w.Path)
	}
	if runtime.GOOS == "windows" {
		if w, _ := pickWorktree(all, `C:\Projects\fincent`, "68fa333", ""); w.Path != "" {
			t.Errorf("picked %q: the same checkout written in another case", w.Path)
		}
	}
	if w, _ := pickWorktree([]worktree{{Path: "C:/x", Bare: true, Head: "abc1234"}}, "C:/root", "abc1234", ""); w.Path != "" {
		t.Errorf("picked the bare clone at %q", w.Path)
	}
}

func TestNewWorktreePathFollowsWhereTheyAlreadyLive(t *testing.T) {
	d := &Doc{Source: &Source{
		Commit: "144da9aa073bdb720c1b9a271504b0e9ec4ea627",
		URL:    "https://github.com/innovadis-dev/Fincent/pull/4164",
	}}
	root := "C:/Projects/Fincent"

	// Two of the three linked worktrees live under the same directory, so a new
	// one goes there too, named after the pull request.
	all := parseWorktrees(worktreeList + `
worktree C:/Projects/Fincent/.claude/worktrees/another
HEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
branch refs/heads/another
`)
	want := filepath.Join("C:/Projects/Fincent/.claude/worktrees", "pr-4164")
	if got := newWorktreePath(root, all, d); got != want {
		t.Errorf("newWorktreePath = %q, want %q", got, want)
	}

	// No worktrees at all: beside the checkout, and then the name has to carry
	// the repository as well, because that directory holds everything.
	only := parseWorktrees("worktree C:/Projects/Fincent\nHEAD 68fa333\nbranch refs/heads/main\n")
	want = filepath.Join("C:/Projects", "Fincent-pr-4164")
	if got := newWorktreePath(root, only, d); got != want {
		t.Errorf("newWorktreePath without any worktrees = %q, want %q", got, want)
	}

	// Not a pull request, so the commit names it.
	d.Source.URL = "https://github.com/innovadis-dev/Fincent"
	want = filepath.Join("C:/Projects", "Fincent-144da9a")
	if got := newWorktreePath(root, only, d); got != want {
		t.Errorf("newWorktreePath outside a pull request = %q, want %q", got, want)
	}
}

func TestFetchCommandUsesThePullRequestRef(t *testing.T) {
	pr := &Doc{Source: &Source{URL: "https://github.com/innovadis-dev/Fincent/pull/4164"}}
	if got := fetchCommand(pr); got != "git fetch origin pull/4164/head" {
		t.Errorf("fetchCommand for a pull request = %q", got)
	}
	if got := fetchCommand(&Doc{Source: &Source{URL: ""}}); got != "git fetch" {
		t.Errorf("fetchCommand outside a pull request = %q", got)
	}
}

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
// so the checkout that was found stays.
func TestCheckoutForLeavesThingsAloneWithoutACommit(t *testing.T) {
	for _, d := range []*Doc{{}, {Source: &Source{}}} {
		root, notes := checkoutFor("C:/Projects/Fincent", false, d)
		if root != "C:/Projects/Fincent" || notes != nil {
			t.Errorf("checkoutFor moved to %q and said %v", root, notes)
		}
	}
	if root, notes := checkoutFor("", false, &Doc{Source: &Source{Commit: "abc"}}); root != "" || notes != nil {
		t.Errorf("checkoutFor without a checkout returned %q and %v", root, notes)
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

	d := &Doc{Source: &Source{Commit: first, URL: "https://github.com/x/y/pull/7"}}

	// With nothing checked out on it, the way to get there is printed and the
	// checkout stays where it is.
	root, notes := checkoutFor(main, false, d)
	if root != main {
		t.Errorf("moved to %q with no worktree to move to", root)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "git worktree add --detach") {
		t.Errorf("did not suggest a worktree:\n%s", joined)
	}
	if strings.Contains(joined, "not here yet") {
		t.Errorf("asked for a fetch of a commit that is right there:\n%s", joined)
	}

	// Now there is one, so that is where it reads.
	side := filepath.Join(dir, "on-the-commit")
	git(main, "worktree", "add", "--detach", side, first)
	root, notes = checkoutFor(main, false, d)
	if !samePath(root, side) {
		t.Fatalf("read against %q, want the worktree at %q", root, side)
	}
	joined = strings.Join(notes, "\n")
	if !strings.Contains(joined, "so that is what will be read") {
		t.Errorf("did not say what it did:\n%s", joined)
	}

	// Unless the reader named a root themselves, which is not overruled.
	root, notes = checkoutFor(main, true, d)
	if root != main {
		t.Errorf("moved away from the root that was asked for, to %q", root)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "would line up better") {
		t.Errorf("did not mention the worktree:\n%s", strings.Join(notes, "\n"))
	}
}
