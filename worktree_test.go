package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	d := &SourceView{
		Commit: "144da9aa073bdb720c1b9a271504b0e9ec4ea627",
		URL:    "https://github.com/innovadis-dev/Fincent/pull/4164",
	}
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
	d.URL = "https://github.com/innovadis-dev/Fincent"
	want = filepath.Join("C:/Projects", "Fincent-144da9a")
	if got := newWorktreePath(root, only, d); got != want {
		t.Errorf("newWorktreePath outside a pull request = %q, want %q", got, want)
	}
}

func TestFetchCommandUsesThePullRequestRef(t *testing.T) {
	pr := &SourceView{URL: "https://github.com/innovadis-dev/Fincent/pull/4164"}
	if got := fetchCommand(pr); got != "git fetch origin pull/4164/head" {
		t.Errorf("fetchCommand for a pull request = %q", got)
	}
	if got := fetchCommand(&SourceView{URL: ""}); got != "git fetch" {
		t.Errorf("fetchCommand outside a pull request = %q", got)
	}
}

func TestFetchArgsAndTheCommandAgree(t *testing.T) {
	pr := &SourceView{URL: "https://github.com/innovadis-dev/Fincent/pull/4164"}
	if got := strings.Join(fetchArgs(pr), " "); got != "fetch origin pull/4164/head" {
		t.Errorf("fetchArgs for a pull request = %q", got)
	}
	if got := fetchCommand(pr); got != "git "+strings.Join(fetchArgs(pr), " ") {
		t.Errorf("the printed command and the one that runs differ: %q", got)
	}
}

// The one field both versions write into, spelled two different ways. Getting
// this wrong means asking a forge about a revision, or reading a branch name as
// one.
func TestLooksLikeRevisionTellsAShaFromABranch(t *testing.T) {
	for _, s := range []string{"2d042da", "144da9aa073bdb720c1b9a271504b0e9ec4ea627", "DEADBEEF"} {
		if !looksLikeRevision(s) {
			t.Errorf("%q did not read as a revision", s)
		}
	}
	for _, s := range []string{"", "main", "feature/FIN-6557-flags", "release-2", "abc123"} {
		if looksLikeRevision(s) {
			t.Errorf("%q read as a revision", s)
		}
	}
}

// A branch the document already carries is never looked up, and neither is
// anything at all when the reader asked for no network.
func TestBranchOfAsksNobodyWhenItDoesNotHaveTo(t *testing.T) {
	cw1 := &SourceView{Head: "feature/FIN-6557-flags", URL: "https://github.com/o/r/pull/12"}
	if b := branchOf(cw1, "", false); b.Name != "feature/FIN-6557-flags" {
		t.Errorf("did not read the branch out of the document: %+v", b)
	}

	// cw/2 writes a revision into the same field, so that one says nothing
	// about a branch and there is no pull request to ask about either.
	cw2 := &SourceView{Head: "144da9aa073bdb720c1b9a271504b0e9ec4ea627", URL: "https://github.com/o/r"}
	if b := branchOf(cw2, "", false); b.Name != "" || b.Note != "" {
		t.Errorf("invented something about a branch: %+v", b)
	}

	pr := &SourceView{Head: "144da9aa073bdb720c1b9a271504b0e9ec4ea627", URL: "https://github.com/o/r/pull/12"}
	b := branchOf(pr, "", true)
	if b.Name != "" || !strings.Contains(b.Note, "--offline") {
		t.Errorf("--offline did not stop the lookup: %+v", b)
	}
}

func TestOfferNotesPutTheFetchFirst(t *testing.T) {
	d := &SourceView{URL: "https://github.com/innovadis-dev/Fincent/pull/4164"}
	o := &offer{Root: "C:/Projects/Fincent", Path: "C:/wt/pr-4164",
		Commit: "144da9aa073bdb720c1b9a271504b0e9ec4ea627", Fetch: fetchArgs(d)}

	notes := offerNotes(o, d)
	if len(notes) != 4 {
		t.Fatalf("said %d things:\n%s", len(notes), strings.Join(notes, "\n"))
	}
	if !strings.Contains(notes[0], "git fetch origin pull/4164/head") {
		t.Errorf("the fetch is not the first thing to do: %q", notes[0])
	}
	if !strings.Contains(notes[2], "git worktree add --detach C:/wt/pr-4164 144da9a") {
		t.Errorf("the command does not match the offer: %q", notes[2])
	}
	if !strings.Contains(notes[3], "gh pr checkout 4164") {
		t.Errorf("did not mention moving the checkout as the other way: %q", notes[3])
	}

	// The commit being here already is one step fewer, and outside a pull
	// request there is no branch to move either.
	o.Fetch, d.URL = nil, "https://github.com/innovadis-dev/Fincent"
	if notes = offerNotes(o, d); len(notes) != 2 {
		t.Errorf("said %d things without a fetch or a pull request:\n%s", len(notes), strings.Join(notes, "\n"))
	}
}

// What cw made it can remove, and what it removed it forgets. Anything else on
// this machine is somebody else's directory.
func TestTheRecordRemembersAndForgets(t *testing.T) {
	store := filepath.Join(t.TempDir(), "worktrees.json")
	worktreeStore = func() (string, error) { return store, nil }

	if all := readMade(); len(all) != 0 {
		t.Fatalf("started with %d records", len(all))
	}
	first := madeWorktree{Path: "C:/wt/pr-1", Root: "C:/Projects/x", Commit: "abc1234", At: time.Now()}
	if err := remember(first); err != nil {
		t.Fatal(err)
	}
	if err := remember(madeWorktree{Path: "C:/wt/pr-2", Root: "C:/Projects/x", Commit: "def5678"}); err != nil {
		t.Fatal(err)
	}
	// The same path twice is one worktree, not two records of it.
	if err := remember(first); err != nil {
		t.Fatal(err)
	}
	if all := readMade(); len(all) != 2 {
		t.Fatalf("kept %d records for two worktrees", len(all))
	}

	forget("C:/wt/pr-1")
	all := readMade()
	if len(all) != 1 || all[0].Path != "C:/wt/pr-2" {
		t.Fatalf("forgetting one left %+v", all)
	}
	forget("C:/wt/pr-2")
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("the empty file stayed behind: %v", err)
	}
}

// The whole of it against a real repository: a worktree gets made, written
// down, read at, and removed again when the server stops. And one with changes
// in it stays, because git refuses to throw work away and neither does this.
func TestAWorktreeIsMadeAndGivenBackAgain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-b", "main")
	git("add", ".")
	git("commit", "-m", "one")
	first := git("rev-parse", "HEAD")

	worktreeStore = func() (string, error) { return filepath.Join(dir, "worktrees.json"), nil }
	made = nil

	o := &offer{Root: main, Path: filepath.Join(dir, "reading"), Commit: first}
	if err := makeWorktree(o, "some-walkthrough", false); err != nil {
		t.Fatalf("makeWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.Path, "a.txt")); err != nil {
		t.Fatalf("the worktree has no files in it: %v", err)
	}
	all := readMade()
	if len(all) != 1 || !samePath(all[0].Path, o.Path) || all[0].Walkthrough != "some-walkthrough" {
		t.Fatalf("wrote down %+v", all)
	}
	if all[0].PID != os.Getpid() {
		t.Errorf("recorded pid %d, which is not this process", all[0].PID)
	}

	// Somebody started working in there, so it stops being a directory nobody
	// asked for.
	scratch := filepath.Join(o.Path, "notes.md")
	if err := os.WriteFile(scratch, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseWorktree()
	if _, err := os.Stat(o.Path); err != nil {
		t.Errorf("removed a worktree with work in it: %v", err)
	}
	if len(readMade()) != 1 {
		t.Errorf("forgot a worktree that is still there")
	}

	// Take that away again and it goes, record and all.
	if err := os.Remove(scratch); err != nil {
		t.Fatal(err)
	}
	made = &all[0]
	releaseWorktree()
	if _, err := os.Stat(o.Path); !os.IsNotExist(err) {
		t.Errorf("the worktree is still at %s: %v", o.Path, err)
	}
	if left := readMade(); len(left) != 0 {
		t.Errorf("kept a record of a worktree that is gone: %+v", left)
	}
	if made != nil {
		t.Errorf("still holds on to one after giving it back")
	}
}
