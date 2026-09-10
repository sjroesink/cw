package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/*
Reading a walkthrough somewhere other than the checkout you work in.

A walkthrough is written against one commit. Read it against another one and
most snippets come back moved or gone, which looks like the walkthrough being
wrong rather than the reader being elsewhere in history. The obvious way out,
gh pr checkout, moves somebody's working tree out from under them and their
uncommitted work with it. A worktree does not: it is a second directory sharing
one object store, and git already knows how to make one.

So cw open works out which branch the walkthrough is about, asks gh when the
document itself does not say, looks whether that branch or that commit is
checked out somewhere already, and offers to add a worktree when neither is. What
it adds it writes down and removes again when the server stops, because a
directory nobody asked for should not outlive the reason it existed. For the
times that does not happen, a kill or a crash, cw worktrees is the way back.
*/

// ---------------------------------------------------------------- the branch

// branchInfo is what could be worked out about the branch a walkthrough is
// about. Every field may be empty. This is a best effort against a forge that
// may be unreachable, and saying nothing beats saying something invented.
type branchInfo struct {
	Name  string // the branch, short form
	Head  string // where that branch is now, when a forge was asked
	State string // open, merged or closed, for a pull request
	Note  string // one line on how this is known, or on why it is not
}

// branchOf answers which branch a walkthrough is about.
//
// cw/1 wrote branch names into source.head and cw/2 writes revisions there, so
// the document knows only sometimes. For a pull request gh knows, and it answers
// with where that branch is now as well. That is not the commit the snippets
// were taken from, which is exactly why it is worth printing.
func branchOf(d *SourceView, root string, offline bool) branchInfo {
	if d == nil {
		return branchInfo{}
	}
	if d.Head != "" && !looksLikeRevision(d.Head) {
		return branchInfo{Name: d.Head, Note: "the walkthrough says it is branch " + d.Head}
	}
	m := prURLPattern.FindStringSubmatch(d.URL)
	if m == nil {
		return branchInfo{}
	}
	pr := "pull request " + m[2]
	if offline {
		return branchInfo{Note: "which branch " + pr + " is on was not looked up, because of --offline"}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return branchInfo{Note: "gh is not on this machine, so which branch " + pr + " is on is unknown"}
	}

	var out struct {
		HeadRefName string `json:"headRefName"`
		HeadRefOid  string `json:"headRefOid"`
		State       string `json:"state"`
	}
	if err := ghJSON(root, &out, "pr", "view", m[2], "--repo", m[1],
		"--json", "headRefName,headRefOid,state"); err != nil {
		return branchInfo{Note: "gh could not say which branch " + pr + " is on: " + err.Error()}
	}

	b := branchInfo{Name: out.HeadRefName, Head: out.HeadRefOid, State: strings.ToLower(out.State)}
	switch {
	case b.Name == "":
		b.Note = "gh named no branch for " + pr
	case b.State == "merged" || b.State == "closed":
		b.Note = fmt.Sprintf("%s is %s, and was on branch %s", pr, b.State, b.Name)
	default:
		b.Note = fmt.Sprintf("%s is on branch %s", pr, b.Name)
	}
	// The branch having moved on is the usual reason checking it out is not the
	// same as reading what this walkthrough describes.
	if b.Head != "" && d.Commit != "" && !sameCommit(b.Head, d.Commit) {
		b.Note += ", which is on " + short(b.Head) + " now"
	}
	return b
}

// looksLikeRevision separates the two things cw/1 and cw/2 both wrote into the
// same field. A branch called deadbeef would read as a revision here, and that
// is the trade for not asking a forge about every walkthrough that opens.
func looksLikeRevision(s string) bool {
	if len(s) < 7 {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF", r)
	}) < 0
}

// ---------------------------------------------------------------- the offer

// offer is a worktree that does not exist yet and would let this walkthrough be
// read at the commit it was written against.
type offer struct {
	Root   string   // the checkout it would be added from
	Path   string   // where it would go
	Commit string   // what it would be put on
	Branch string   // which branch that commit belongs to, when that is known
	Fetch  []string // git arguments to run first, empty when the commit is here
}

// offerNotes is the same worktree said as commands, for when it is not going to
// be made here: no terminal to ask at, --no-worktree, or an answer of no.
func offerNotes(o *offer, d *SourceView) []string {
	var notes []string
	if len(o.Fetch) > 0 {
		notes = append(notes, "that commit is not here yet: git "+strings.Join(o.Fetch, " "))
	}
	notes = append(notes,
		"a worktree reads it without touching this checkout:",
		"    git worktree add --detach "+o.Path+" "+short(o.Commit))
	if d != nil {
		if m := prURLPattern.FindStringSubmatch(d.URL); m != nil {
			notes = append(notes, "or move this checkout instead: gh pr checkout "+m[2])
		}
	}
	return notes
}

// worktreeMode is what cw open does about a worktree it would have to make.
type worktreeMode int

const (
	worktreeAsk worktreeMode = iota // ask, when there is somebody to ask
	worktreeYes                     // --worktree
	worktreeNo                      // --no-worktree
)

// offerWorktree asks, and does what the answer says. It gives back the path to
// read at, or nothing, and then the way to do it by hand is printed instead.
func offerWorktree(o *offer, d *SourceView, name string, mode worktreeMode, offline bool) string {
	switch {
	case mode == worktreeNo:
	case mode == worktreeYes, askYes("  add a worktree at " + o.Path + " and read it there?"):
		if err := makeWorktree(o, name, offline); err != nil {
			fmt.Printf("  %v\n", err)
			break
		}
		fmt.Printf("  added the worktree at %s, and it is removed again when this server stops\n", o.Path)
		return o.Path
	}
	for _, line := range offerNotes(o, d) {
		fmt.Printf("  %s\n", line)
	}
	return ""
}

// makeWorktree adds it, and writes down that cw made it. The record is what
// lets something clean up after a run that never got to.
func makeWorktree(o *offer, name string, offline bool) error {
	if len(o.Fetch) > 0 {
		if offline {
			return fmt.Errorf("%s is not in this checkout and --offline says not to fetch it", short(o.Commit))
		}
		fmt.Printf("  git %s\n", strings.Join(o.Fetch, " "))
		if out, err := gitIn(o.Root, o.Fetch...); err != nil {
			return fmt.Errorf("that fetch failed, so no worktree was made: %s", gitSaid(out, err))
		}
	}
	if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
		return fmt.Errorf("%s cannot be made: %w", filepath.Dir(o.Path), err)
	}
	out, err := gitIn(o.Root, "worktree", "add", "--detach", o.Path, o.Commit)
	if err != nil {
		return fmt.Errorf("git worktree add: %s", gitSaid(out, err))
	}

	made = &madeWorktree{
		Path: o.Path, Root: o.Root, Commit: o.Commit,
		Walkthrough: name, PID: os.Getpid(), At: time.Now().UTC(),
	}
	if err := remember(*made); err != nil {
		fmt.Fprintf(os.Stderr, "cw: the worktree was made but not written down, so cw worktrees will not find it: %v\n", err)
	}
	return nil
}

// ---------------------------------------------------------------- the record

// madeWorktree is one directory cw added, and enough about it to remove it
// again from another process.
type madeWorktree struct {
	Path        string    `json:"path"`
	Root        string    `json:"root"`
	Commit      string    `json:"commit"`
	Walkthrough string    `json:"walkthrough,omitempty"`
	PID         int       `json:"pid"`
	At          time.Time `json:"at"`
}

// made is what this process added, if it added one. At most one: cw open serves
// a single walkthrough.
var made *madeWorktree

// worktreeStore is where the record lives, beside the settings rather than with
// the secrets, because it is neither secret nor about one walkthrough. A var so
// a test can point it somewhere temporary.
var worktreeStore = func() (string, error) {
	p, err := settingsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "worktrees.json"), nil
}

var madeMu sync.Mutex

func readMade() []madeWorktree {
	p, err := worktreeStore()
	if err != nil {
		return nil
	}
	var all []madeWorktree
	if err := readJSONFile(p, &all); err != nil {
		return nil
	}
	return all
}

func writeMade(all []madeWorktree) error {
	p, err := worktreeStore()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeJSONFile(p, all)
}

func remember(m madeWorktree) error {
	madeMu.Lock()
	defer madeMu.Unlock()
	return writeMade(append(without(readMade(), m.Path), m))
}

func forget(path string) {
	madeMu.Lock()
	defer madeMu.Unlock()
	_ = writeMade(without(readMade(), path))
}

func without(all []madeWorktree, path string) []madeWorktree {
	var kept []madeWorktree
	for _, m := range all {
		if !samePath(m.Path, path) {
			kept = append(kept, m)
		}
	}
	return kept
}

// releaseWorktree removes what this run added. It is a no-op for every run that
// added nothing, which is nearly all of them.
//
// git refuses to remove a worktree with changes in it, and that refusal is kept
// rather than forced past: somebody who started editing in there meant it.
func releaseWorktree() {
	if made == nil {
		return
	}
	m := *made
	made = nil
	if err := removeMade(m); err != nil {
		fmt.Printf("the worktree at %s is still there: %v\n", m.Path, err)
		fmt.Printf("remove it later with cw worktrees clean, or keep it\n")
		return
	}
	fmt.Printf("removed the worktree at %s\n", m.Path)
}

func removeMade(m madeWorktree) error {
	if _, err := os.Stat(m.Path); os.IsNotExist(err) {
		forget(m.Path)
		return nil
	}
	if out, err := gitIn(m.Root, "worktree", "remove", m.Path); err != nil {
		return errors.New(gitSaid(out, err))
	}
	forget(m.Path)
	return nil
}

// ---------------------------------------------------------------- the command

func cmdWorktrees(args []string) {
	clean := false
	for _, a := range args {
		switch a {
		case "list":
		case "clean", "remove", "--clean":
			clean = true
		default:
			die("usage: cw worktrees [clean]")
		}
	}

	// One that is gone already is not news, so it is dropped rather than
	// listed. That also keeps the file from growing forever.
	all, live := readMade(), []madeWorktree{}
	for _, m := range all {
		if _, err := os.Stat(m.Path); err == nil {
			live = append(live, m)
		}
	}
	if len(live) != len(all) {
		madeMu.Lock()
		_ = writeMade(live)
		madeMu.Unlock()
	}
	if len(live) == 0 {
		fmt.Println("no worktrees made by cw open are left")
		return
	}

	if !clean {
		fmt.Printf("%d worktree(s) made by cw open:\n", len(live))
		for _, m := range live {
			fmt.Printf("  %s\n", m.Path)
			what := "on " + short(m.Commit)
			if m.Walkthrough != "" {
				what += ", for " + m.Walkthrough
			}
			fmt.Printf("      %s, added %s from %s\n", what, ago(m.At), m.Root)
		}
		fmt.Printf("\nremove them with cw worktrees clean\n")
		return
	}

	kept := 0
	for _, m := range live {
		if err := removeMade(m); err != nil {
			fmt.Printf("  kept %s: %v\n", m.Path, err)
			kept++
			continue
		}
		fmt.Printf("  removed %s\n", m.Path)
	}
	if kept > 0 {
		fmt.Printf("\n%d of them have changes in them, which git will not throw away and neither will this.\n", kept)
	}
}

// ---------------------------------------------------------------- worktrees

// worktree is one entry of git worktree list --porcelain.
type worktree struct {
	Path   string
	Head   string
	Branch string // the short name, empty when detached
	Bare   bool
}

func worktreesOf(root string) []worktree {
	out, err := runIn(root, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	return parseWorktrees(out)
}

// parseWorktrees reads the porcelain form, which is groups of lines separated
// by a blank one, each starting with the path.
func parseWorktrees(out string) []worktree {
	var all []worktree
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, worktree{Path: filepath.Clean(strings.TrimPrefix(line, "worktree "))})
		case len(all) == 0:
			// Anything before the first path belongs to nothing.
		case strings.HasPrefix(line, "HEAD "):
			all[len(all)-1].Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			all[len(all)-1].Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			all[len(all)-1].Bare = true
		}
	}
	return all
}

// pickWorktree chooses the checkout that will make the snippets line up.
// Sitting on exactly the commit is worth more than being on the right branch,
// because a branch that has moved on since is how snippets drift in the first
// place.
func pickWorktree(all []worktree, root, commit, branch string) (worktree, string) {
	usable := func(w worktree) bool { return !w.Bare && w.Path != "" && !samePath(w.Path, root) }
	for _, w := range all {
		if usable(w) && sameCommit(w.Head, commit) {
			return w, "on " + short(commit)
		}
	}
	if branch != "" {
		for _, w := range all {
			if usable(w) && w.Branch == branch {
				return w, "on " + branch
			}
		}
	}
	return worktree{}, ""
}

// newWorktreePath suggests where a new one would go, following wherever this
// repository already keeps its worktrees rather than inventing a convention
// for somebody. Only when there are none does it fall back to a directory
// beside the checkout, and then the name carries the repository too.
func newWorktreePath(root string, all []worktree, d *SourceView) string {
	name := short(d.Commit)
	if m := prURLPattern.FindStringSubmatch(d.URL); m != nil {
		name = "pr-" + m[2]
	}

	counts := map[string]int{}
	best, most := "", 0
	for _, w := range all {
		if w.Bare || w.Path == "" || samePath(w.Path, root) {
			continue
		}
		parent := filepath.Dir(w.Path)
		counts[parent]++
		if counts[parent] > most {
			best, most = parent, counts[parent]
		}
	}
	if best == "" {
		return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+name)
	}
	return filepath.Join(best, name)
}

// fetchArgs is what brings the commit into the object store. A pull request has
// a ref of its own, which works for a fork as well, where fetching the branch by
// name would not.
func fetchArgs(d *SourceView) []string {
	if m := prURLPattern.FindStringSubmatch(d.URL); m != nil {
		return []string{"fetch", "origin", "pull/" + m[2] + "/head"}
	}
	return []string{"fetch"}
}

func fetchCommand(d *SourceView) string {
	return "git " + strings.Join(fetchArgs(d), " ")
}

// ---------------------------------------------------------------- odds and ends

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitSaid picks the line of git output worth repeating. The failure is on the
// last line, after whatever progress it printed on the way there.
func gitSaid(out string, err error) string {
	if line := lastLine(out); line != "" {
		return line
	}
	return err.Error()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// askPatience is how long a question waits for its answer. A console with
// nobody in front of it looks exactly like one with somebody thinking about it,
// so the only way to tell them apart is to stop waiting.
const askPatience = 30 * time.Second

// askYes puts a question to whoever is at the terminal. With nobody there, and
// that is every run from a script or an agent, the answer is no and the command
// to do it by hand gets printed instead.
//
// Never waiting forever is the part that matters. cw open that hangs on a
// prompt is worse than cw open that does not offer anything, because the page
// it was asked for never gets served at all.
func askYes(question string) bool {
	if !atATerminal() {
		return false
	}
	fmt.Printf("%s [y/N] ", question)

	said := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return
		}
		said <- strings.ToLower(strings.TrimSpace(line))
	}()
	select {
	case line := <-said:
		switch line {
		case "y", "yes", "j", "ja":
			return true
		}
	case <-time.After(askPatience):
		fmt.Printf("\n  nobody answered, so this is what it would have been:\n")
	}
	return false
}

// atATerminal wants both ends of it: something to read the answer from, and
// somewhere the question is actually legible. Output redirected to a file with
// a console still on the input side is how a background run ends up waiting on
// a question that was never on anybody's screen.
func atATerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		st, err := f.Stat()
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

// ago is how long ago something was, in the roughest unit that still says
// something.
func ago(t time.Time) string {
	if t.IsZero() {
		return "at some point"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minute(s) ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hour(s) ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d day(s) ago", int(d.Hours()/24))
}
