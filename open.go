package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/*
Reading a published walkthrough with the buttons that only work locally.

The hosted page cannot open a file in your editor and cannot check the snippets
against your checkout, because it has neither. Rather than teaching the browser
to reach into your machine, this pulls the walkthrough the other way: fetch what
was published, serve it on 127.0.0.1, and you get the local page over hosted
content. Which is the whole local reader, unchanged.

Two things decide whether that reads well, and both go wrong quietly. The
checkout has to be the repository the walkthrough is about, and it has to be at
roughly the commit the snippets were taken from. Get either wrong and every
snippet reports as missing, which looks like the walkthrough being broken rather
than the reader being on the wrong branch. So both are worked out here and said
out loud.
*/

func cmdOpen(args []string) {
	site := strings.TrimRight(envOr("CW_SITE", defaultSite), "/")
	f := flags{port: -1}
	target, password := "", strings.TrimSpace(os.Getenv("CW_PASSWORD"))
	mode := worktreeAsk

	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("%s needs a value", a)
			}
			i++
			return args[i]
		}
		switch {
		case a == "--root":
			f.root = next()
		case strings.HasPrefix(a, "--root="):
			f.root = strings.TrimPrefix(a, "--root=")
		case a == "--site":
			site = strings.TrimRight(next(), "/")
		case strings.HasPrefix(a, "--site="):
			site = strings.TrimRight(strings.TrimPrefix(a, "--site="), "/")
		case a == "--port":
			f.port = atoiOr(next(), -1)
		case strings.HasPrefix(a, "--port="):
			f.port = atoiOr(strings.TrimPrefix(a, "--port="), -1)
		case a == "--no-open":
			f.noOpen = true
		case a == "--offline":
			f.offline = true
		case a == "--worktree":
			mode = worktreeYes
		case a == "--no-worktree":
			mode = worktreeNo
		case a == "--password":
			password = next()
		case strings.HasPrefix(a, "--password="):
			password = strings.TrimPrefix(a, "--password=")
		case strings.HasPrefix(a, "-"):
			die("unknown flag %q", a)
		default:
			if target != "" {
				die("give one walkthrough, got %q and %q", target, a)
			}
			target = a
		}
	}
	if target == "" {
		die("give a walkthrough URL or its name")
	}

	url := apiURL(target, site)
	// Your own walkthrough opens with the key you were given when you published
	// it, so a password you set yourself is not something you have to type back
	// at yourself.
	slug := url[strings.LastIndexByte(url, '/')+1:]
	doc, name, err := fetchDoc(url, password, editKeyFor(site, slug))
	if err != nil {
		die("%v", err)
	}

	// Without --root, find the checkout the walkthrough is about. Guessing
	// wrong is worse than not guessing, so a candidate only counts when its
	// origin remote actually names that repository.
	guessed := ""
	if f.root == "" {
		if guessed = guessRoot(doc.Source); guessed != "" {
			f.root = guessed
		}
	}

	// The file is a copy of what is published, so it goes somewhere temporary
	// rather than into whatever directory this was run from.
	dir, err := os.MkdirTemp("", "cw-open-")
	if err != nil {
		die("%v", err)
	}
	// Whatever root the publisher had is meaningless here, and letting it
	// resolve against a temp directory would point the open buttons at
	// somewhere arbitrary. Only --root decides where the code is on this
	// machine.
	if doc.V1 != nil {
		doc.V1.Root = ""
	}
	path := filepath.Join(dir, name+".json")
	if err := WriteDoc(path, doc.Document()); err != nil {
		die("%v", err)
	}

	// Which checkout to read this against is not always the one that was
	// found. A worktree already sitting on the right commit is a better answer
	// than telling somebody to move the branch they are working on, and when
	// there is no such worktree, making one is offered rather than described.
	plan := checkoutFor(f.root, guessed == "" && f.root != "", doc.Source,
		func() branchInfo { return branchOf(doc.Source, f.root, f.offline) })
	f.root = plan.Root

	fmt.Printf("%s\n  fetched from %s\n", doc.Title, url)
	if guessed != "" {
		fmt.Printf("  found the checkout at %s\n", guessed)
	}
	if f.root == "" {
		fmt.Printf("  no checkout found, so nothing will be opened or checked. Give one with --root\n")
	}
	for _, line := range plan.Notes {
		fmt.Printf("  %s\n", line)
	}
	switch {
	case plan.Add != nil:
		if added := offerWorktree(plan.Add, doc.Source, slug, mode, f.offline); added != "" {
			f.root = added
		}
	case plan.Move != nil:
		offerCatchUp(plan.Move, mode, f.offline)
	}
	fmt.Println()

	// The name it was published under is what its comments are filed under
	// too, so a question asked yesterday is still there when it is opened
	// again.
	f.slug = slug
	f.file = path
	runServe(f)
}

// apiURL takes whatever the reader pasted and works out what to fetch: a page
// URL, an API URL, or a bare name.
func apiURL(target, site string) string {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return site + "/api/v1/walkthroughs/" + target
	}
	target = strings.TrimRight(target, "/")
	if i := strings.Index(target, "/w/"); i >= 0 {
		return target[:i] + "/api/v1/walkthroughs/" + target[i+len("/w/"):]
	}
	return target
}

func fetchDoc(url, password, key string) (*Walkthrough, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	if password != "" {
		req.Header.Set("X-Cw-Password", password)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, "", fmt.Errorf("%s is protected. Give the password with --password, or set CW_PASSWORD", url)
	case http.StatusForbidden:
		return nil, "", fmt.Errorf("%s does not allow this machine to read it. Ask whoever published it to add your address", url)
	default:
		return nil, "", fmt.Errorf("%s answered %s", url, resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", err
	}
	var body struct {
		Doc  json.RawMessage `json:"doc"`
		Meta *Meta           `json:"meta"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Doc) == 0 {
		return nil, "", fmt.Errorf("%s did not answer with a walkthrough", url)
	}
	// The document is read here rather than trusted, so a walkthrough written
	// in a version this build does not know says so now, with a sentence, and
	// not by rendering as an empty page.
	res, err := ParseDoc(body.Doc, url)
	if err != nil {
		return nil, "", err
	}
	name := "walkthrough"
	if body.Meta != nil && body.Meta.Slug != "" {
		name = body.Meta.Slug
	}
	return res.View(), name, nil
}

// ---------------------------------------------------------------- the checkout

// guessRoot looks for the repository a walkthrough is about. Every candidate is
// confirmed against its origin remote rather than against its directory name,
// because a directory called Fincent that is a different repository would send
// every open button somewhere wrong.
func guessRoot(d *SourceView) string {
	if d == nil || d.Repo == "" {
		return ""
	}
	want := strings.ToLower(strings.Trim(d.Repo, "/"))

	cwd, _ := os.Getwd()
	var candidates []string
	// The repository this was run from, whichever directory inside it.
	if out, err := runIn(cwd, "git", "rev-parse", "--show-toplevel"); err == nil {
		candidates = append(candidates, strings.TrimSpace(out))
	}
	// Then the places checkouts usually live, by the repository's own name.
	name := want
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(filepath.Dir(cwd), name),
		filepath.Join(home, "Projects", name),
		filepath.Join(home, "src", name),
		filepath.Join("C:\\Projects", name),
	} {
		candidates = append(candidates, dir)
	}

	for _, dir := range candidates {
		if root := confirm(dir, want); root != "" {
			return root
		}
	}

	// A checkout is often not named after its repository: innovadis-shared/cw
	// lives in CodeWalkthrough here. So the last resort is to look one level
	// down the places checkouts live and ask each one what it is.
	for _, parent := range []string{filepath.Dir(cwd), filepath.Join(home, "Projects"), "C:\\Projects"} {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for i, e := range entries {
			if i >= 60 {
				break
			}
			if !e.IsDir() {
				continue
			}
			if root := confirm(filepath.Join(parent, e.Name()), want); root != "" {
				return root
			}
		}
	}
	return ""
}

// confirm turns a candidate directory into a root, or into nothing.
func confirm(dir, want string) string {
	if dir == "" {
		return ""
	}
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || (!st.IsDir() && st.Size() == 0) {
		return ""
	}
	if !remoteNames(dir, want) {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return abs
}

// remoteNames says whether this checkout's origin is the repository we are
// after, over both URL shapes git uses.
func remoteNames(dir, want string) bool {
	out, err := runIn(dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return false
	}
	url := strings.ToLower(strings.TrimSpace(out))
	url = strings.TrimSuffix(url, ".git")
	return strings.HasSuffix(url, "/"+want) || strings.HasSuffix(url, ":"+want)
}

// checkoutPlan is what checkoutFor decided: where the walkthrough is going to
// be read, what to say about that, and at most one thing to ask the reader for.
type checkoutPlan struct {
	Root  string   // the checkout to read against
	Notes []string // what to print about the choice
	Add   *offer   // a worktree that does not exist yet
	Move  *catchUp // one that does, on the right branch behind the commit
}

// checkoutFor works out which of this machine's checkouts to read the
// walkthrough against, and what to say about the choice.
//
// A checkout on another commit is the usual reason a walkthrough reads as
// broken: the files the change adds are not there yet, so most snippets come
// back missing and it looks like the walkthrough is wrong rather than the
// reader being elsewhere in history. The obvious way out, gh pr checkout, moves
// somebody's working tree out from under them. A worktree does not, and there
// is often already one, so the repository's other checkouts are looked at
// before anything is asked of the reader.
//
// pinned says the reader named the root themselves. Then it is not moved:
// being told about a better checkout is help, being sent to a different one
// than you asked for is not. A worktree that gets made along the way is
// different again, because that one was agreed to out loud.
//
// branch is asked for the branch this walkthrough is about, and it is a
// function rather than a value because answering it can mean a call to a forge.
// A checkout that already lines up asks nobody anything.
func checkoutFor(root string, pinned bool, d *SourceView, branch func() branchInfo) checkoutPlan {
	if root == "" || d == nil {
		return checkoutPlan{Root: root}
	}
	br, asked := branchInfo{}, false
	about := func() branchInfo {
		if !asked && branch != nil {
			br, asked = branch(), true
		}
		return br
	}

	// A walkthrough written by cw always records the revision it was taken
	// from. One that does not still has a branch, and where that branch is now
	// is the closest thing there is to an answer.
	want, byBranch := d.Commit, false
	if want == "" {
		want, byBranch = about().Head, true
		if want == "" {
			return checkoutPlan{Root: root}
		}
	}

	head := ""
	if out, err := runIn(root, "git", "rev-parse", "HEAD"); err == nil {
		head = strings.TrimSpace(out)
	}
	if head == "" || sameCommit(head, want) {
		return checkoutPlan{Root: root}
	}

	notes := []string{fmt.Sprintf("this was written against %s and the checkout is on %s",
		short(want), short(head))}
	if byBranch {
		notes = []string{fmt.Sprintf("this records no revision, so branch %s as it is now (%s) is what it is read against, and the checkout is on %s",
			about().Name, short(want), short(head))}
	} else if n := about().Note; n != "" {
		notes = append(notes, n)
	}

	all := worktreesOf(root)
	if w, why := pickWorktree(all, root, want, about().Name); w.Path != "" {
		if pinned {
			return checkoutPlan{Root: root, Notes: append(notes,
				fmt.Sprintf("a worktree at %s is %s, which would line up better", w.Path, why))}
		}
		notes = append(notes, fmt.Sprintf("a worktree at %s is %s, so that is what will be read", w.Path, why))
		if sameCommit(w.Head, want) {
			return checkoutPlan{Root: w.Path, Notes: notes}
		}

		// On the branch but not on the commit, which is the ordinary state of a
		// worktree somebody made a while ago: the branch moved and it did not.
		// Nothing new has to be made for that, one step forward is enough.
		notes = append(notes, fmt.Sprintf("it is on %s though, so a snippet may still have moved", short(w.Head)))
		move, reason := planCatchUp(root, w, d, want)
		if reason != "" {
			notes = append(notes, reason)
		}
		return checkoutPlan{Root: w.Path, Notes: notes, Move: move}
	}

	// Nothing checked out anywhere near it. A new worktree is the way there
	// that costs this checkout nothing, so that is what gets offered.
	o := &offer{Root: root, Path: newWorktreePath(root, all, d), Commit: want, Branch: about().Name}
	if _, err := runIn(root, "git", "cat-file", "-e", want+"^{commit}"); err != nil {
		o.Fetch = fetchArgs(d)
	}
	return checkoutPlan{Root: root, Notes: notes, Add: o}
}

func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// samePath compares two checkouts. Case-insensitively on Windows, where git
// answers C:/Projects/Fincent for the directory a reader called C:\Projects\fincent.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
