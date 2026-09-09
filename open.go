package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		if guessed = guessRoot(doc); guessed != "" {
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
	doc.Root = ""
	path := filepath.Join(dir, name+".json")
	if err := WriteDoc(path, doc); err != nil {
		die("%v", err)
	}

	fmt.Printf("%s\n  fetched from %s\n", doc.Title, url)
	if guessed != "" {
		fmt.Printf("  found the checkout at %s\n", guessed)
	}
	if f.root == "" {
		fmt.Printf("  no checkout found, so nothing will be opened or checked. Give one with --root\n")
	}
	for _, line := range commitNotes(f.root, doc) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

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

func fetchDoc(url, password, key string) (*Doc, string, error) {
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
		Doc  *Doc  `json:"doc"`
		Meta *Meta `json:"meta"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Doc == nil {
		return nil, "", fmt.Errorf("%s did not answer with a walkthrough", url)
	}
	name := "walkthrough"
	if body.Meta != nil && body.Meta.Slug != "" {
		name = body.Meta.Slug
	}
	return body.Doc, name, nil
}

// ---------------------------------------------------------------- the checkout

// guessRoot looks for the repository a walkthrough is about. Every candidate is
// confirmed against its origin remote rather than against its directory name,
// because a directory called Fincent that is a different repository would send
// every open button somewhere wrong.
func guessRoot(d *Doc) string {
	if d.Source == nil || d.Source.Repo == "" {
		return ""
	}
	want := strings.ToLower(strings.Trim(d.Source.Repo, "/"))

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

	// A checkout is often not named after its repository: sjroesink/cw lives in
	// CodeWalkthrough here. So the last resort is to look one level down the
	// places checkouts live and ask each one what it is.
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

// commitNotes says how far the checkout is from what the walkthrough describes.
// Being on another branch is the usual reason a walkthrough looks broken
// locally, so the way out is printed rather than left to be worked out.
func commitNotes(root string, d *Doc) []string {
	if root == "" || d.Source == nil || d.Source.Commit == "" {
		return nil
	}
	head := ""
	if out, err := runIn(root, "git", "rev-parse", "HEAD"); err == nil {
		head = strings.TrimSpace(out)
	}
	if head == "" || strings.HasPrefix(head, d.Source.Commit) || strings.HasPrefix(d.Source.Commit, head) {
		return nil
	}

	notes := []string{fmt.Sprintf("this was written against %s and the checkout is on %s",
		short(d.Source.Commit), short(head))}

	// A commit that is not even in the object store means a fetch first, which
	// is worth knowing before trying to check anything out.
	if _, err := runIn(root, "git", "cat-file", "-e", d.Source.Commit+"^{commit}"); err != nil {
		notes = append(notes, "that commit is not in this checkout yet: git fetch")
	}
	if m := prURLPattern.FindStringSubmatch(d.Source.URL); m != nil {
		notes = append(notes, "to read it against the change itself: gh pr checkout "+m[2])
	} else {
		notes = append(notes, "to read it against the code it describes: git checkout "+short(d.Source.Commit))
	}
	return notes
}
