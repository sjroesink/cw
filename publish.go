package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

/*
Publishing from the machine the walkthrough was written on, which is the only
machine that can say anything about whether the snippets are still true. The
site has no working tree, so this is where the code is checked, where the commit
is read out of git, and where the ids and snippet anchors are filled in.

The one mistake worth designing against is publishing twice and ending up with
two walkthroughs while the link you already sent round still shows the old one.
So the first publish leaves a note beside the file, and every publish after that
updates in place unless you say otherwise.

Publishing itself needs no key. What comes back is a key for that one
walkthrough, and it is the only thing that can change it afterwards. It goes
straight into the secrets directory rather than into the note beside the file,
because the note sits in a repository and sooner or later somebody commits it.
*/

const defaultSite = "https://cw.roesink.dev"

type publishFlags struct {
	file  string
	root  string
	site  string
	slug  string
	isNew bool
	force bool

	// The lock. Nil means leave it as it is, which is what makes republishing
	// a protected walkthrough safe; a flag that was given, even empty, sets it.
	password *string
	allow    *[]string
}

// published is the note left beside a walkthrough after it goes out: which site
// it went to and under which name. It is deliberately not in the walkthrough
// itself, because where a document happens to be hosted is not part of it.
type published struct {
	Site string    `json:"site"`
	Slug string    `json:"slug"`
	URL  string    `json:"url"`
	At   time.Time `json:"at"`
}

func cmdPublish(args []string) {
	f := publishFlags{site: envOr("CW_SITE", defaultSite)}
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
			f.site = next()
		case strings.HasPrefix(a, "--site="):
			f.site = strings.TrimPrefix(a, "--site=")
		case a == "--slug":
			f.slug = next()
		case strings.HasPrefix(a, "--slug="):
			f.slug = strings.TrimPrefix(a, "--slug=")
		case a == "--new":
			f.isNew = true
		case a == "--force":
			f.force = true
		case a == "--password":
			v := next()
			f.password = &v
		case strings.HasPrefix(a, "--password="):
			v := strings.TrimPrefix(a, "--password=")
			f.password = &v
		case a == "--no-password":
			v := ""
			f.password = &v
		case a == "--allow":
			v := splitList(next())
			f.allow = &v
		case strings.HasPrefix(a, "--allow="):
			v := splitList(strings.TrimPrefix(a, "--allow="))
			f.allow = &v
		case a == "--no-allow":
			v := []string{}
			f.allow = &v
		case strings.HasPrefix(a, "-"):
			die("unknown flag %q", a)
		default:
			if f.file != "" {
				die("give one walkthrough file, got %q and %q", f.file, a)
			}
			f.file = a
		}
	}
	if f.file == "" {
		die("give a walkthrough file")
	}
	f.site = strings.TrimRight(f.site, "/")
	// A password on a command line ends up in shell history, so the
	// environment is the better way in and is read when no flag was given.
	if f.password == nil {
		if v, set := os.LookupEnv("CW_PASSWORD"); set {
			f.password = &v
		}
	}
	if f.allow != nil && len(*f.allow) > 0 {
		if _, err := parseNets(*f.allow); err != nil {
			die("%v", err)
		}
	}

	res, err := LoadDoc(f.file)
	if err != nil {
		die("%v", err)
	}
	if len(res.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "%d error(s), so nothing was published:\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		os.Exit(1)
	}
	view := res.View()

	// The tree check is the whole reason to publish from here rather than by
	// posting the file from anywhere.
	root := resolveRoot(flags{file: f.file, root: f.root}, view.RootHint)
	tree := &Tree{Root: root}
	checked, moved, stale := tree.Verify(view)
	if root == "" {
		fmt.Fprintf(os.Stderr, "cw: no working tree was found, so the snippets were not checked. "+
			"The page will say the code is unverified.\n")
	}
	if stale > 0 && !f.force {
		fmt.Fprintf(os.Stderr, "cw: %d snippet(s) are no longer in the working tree. Run cw check to see which, "+
			"fix them, or publish with --force to say it out loud on the page.\n", stale)
		os.Exit(1)
	}

	if view.V2 != nil {
		fillSource2(view.V2, root)
		EnsureAnchors2(view.V2)
	} else {
		fillSource(view.V1, root)
		EnsureIDs(view.V1)
		EnsureAnchors(view.V1)
	}
	// Filling in the source moves what the link builder reads, so the view is
	// taken again rather than answering from before.
	view = res.View()

	body, err := view.Raw()
	if err != nil {
		die("%v", err)
	}
	target, method, slug := f.resolveTarget()

	// A first publish that says nothing about a password gets one, unless the
	// repository it is about is public. Republishing never touches the lock:
	// f.password stays nil and the site keeps whatever it had.
	made, lock := "", ""
	if f.password == nil && method == http.MethodPost {
		public, why := RepoIsPublic(repoOf(view, root), root)
		if pw, note := defaultLock(public, why); pw != nil {
			made, f.password, lock = *pw, pw, note
		} else {
			lock = note
		}
	}

	// Anything about who may read it travels in an envelope around the
	// document, because it is about this copy on this site and not about the
	// walkthrough itself.
	if f.password != nil || f.allow != nil || f.slug != "" {
		body, err = json.Marshal(envelope{
			Slug: f.slug, Walkthrough: body, Password: f.password, Allow: f.allow,
		})
		if err != nil {
			die("%v", err)
		}
	}

	key := ""
	if method == http.MethodPut {
		if key = editKeyFor(f.site, slug); key == "" {
			die("nothing here knows the key for %s. It was printed once, when it was published. "+
				"Set CW_API_KEY to it, or publish a new one with --new", slug)
		}
	}
	out, err := send(method, target, key, body)
	if err != nil {
		die("%v", err)
	}
	if out.Key != "" {
		if err := rememberEditKey(f.site, out.Slug, out.Key); err != nil {
			fmt.Fprintf(os.Stderr, "cw: could not store the edit key, so write it down now: %v\n", err)
			fmt.Printf("  edit key  %s\n", out.Key)
		}
	}
	if made != "" {
		if err := rememberPassword(f.site, out.Slug, made); err != nil {
			fmt.Fprintf(os.Stderr, "cw: could not store the password, so write it down now: %v\n", err)
		}
	}

	fmt.Printf("%s\n", out.URL)
	fmt.Printf("  %s\n", view.Title)
	if root != "" {
		fmt.Printf("  %d snippet(s) checked: %d moved, %d no longer there\n", checked, moved, stale)
	}
	if c := commitOf(view); c != "" {
		fmt.Printf("  against commit %s\n", short(c))
	}
	if lock != "" {
		fmt.Printf("  %s\n", lock)
	}
	if made != "" {
		path, _ := passwordPath()
		fmt.Printf("  the password is %s, and it is in %s\n", made, path)
	}
	if f.password != nil && made == "" {
		if *f.password == "" {
			fmt.Printf("  no password on it any more\n")
		} else {
			fmt.Printf("  password set, and readers will be asked for it\n")
		}
	}
	if f.allow != nil {
		if len(*f.allow) == 0 {
			fmt.Printf("  readable from anywhere again\n")
		} else {
			fmt.Printf("  readable only from %s\n", strings.Join(*f.allow, ", "))
		}
	}
	if out.Key != "" {
		path, _ := editKeyPath()
		fmt.Printf("  the key that can change it is in %s\n", path)
	}
	if out.Message != "" {
		fmt.Printf("  %s\n", out.Message)
	}
	for _, w := range out.Warnings {
		fmt.Printf("  warning: %s\n", w)
	}
	rememberPublish(f.file, published{Site: f.site, Slug: out.Slug, URL: out.URL, At: time.Now().UTC()})
}

// resolveTarget decides between making a new walkthrough and replacing one. An
// explicit --slug wins, then the note left by the last publish, and only a file
// that has never been published, or --new, makes a second one.
func (f publishFlags) resolveTarget() (url, method, slug string) {
	base := f.site + "/api/v1/walkthroughs"
	if f.isNew {
		return base, http.MethodPost, ""
	}
	if f.slug != "" {
		return base + "/" + f.slug, http.MethodPut, f.slug
	}
	if prev, ok := lastPublish(f.file); ok && prev.Site == f.site && prev.Slug != "" {
		fmt.Printf("updating %s in place. Use --new for a second one\n", prev.Slug)
		return base + "/" + prev.Slug, http.MethodPut, prev.Slug
	}
	return base, http.MethodPost, ""
}

func send(method, url, key string, body []byte) (*apiResult, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	var out apiResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s answered with %s, not with JSON: %s", url, resp.Status, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode != http.StatusOK {
		msg := out.Message
		if msg == "" {
			msg = resp.Status
		}
		for _, e := range out.Errors {
			msg += "\n  - " + e
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return &out, nil
}

// ---------------------------------------------------------------- the keys

/*
Two kinds, and only the first is normal. A walkthrough's own key comes back when
it is published and can change that one walkthrough. An admin key belongs to
whoever runs the site and works on everything; it is how a lost key is recovered.

Both are secrets, so neither is ever printed, passed as an argument, or written
anywhere near a repository. They live under the secrets directory, and the key
for a walkthrough is looked up by the site it is on plus its name, so the same
walkthrough published to two sites keeps two keys.
*/

func secretPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "secrets", name), nil
}

func editKeyPath() (string, error)  { return secretPath("cw-keys.json") }
func passwordPath() (string, error) { return secretPath("cw-passwords.json") }

func editKeyID(site, slug string) string {
	return strings.TrimRight(site, "/") + "/" + slug
}

// editKeyFor answers with the key that may change this walkthrough: an explicit
// CW_API_KEY first, so a one-off or a pipeline can hand one in, then the one
// stored when it was published, then the admin key if there is one.
func editKeyFor(site, slug string) string {
	if k := strings.TrimSpace(os.Getenv("CW_API_KEY")); k != "" {
		return k
	}
	path, err := editKeyPath()
	if err == nil {
		keys := map[string]string{}
		if readJSONFile(path, &keys) == nil {
			if k := strings.TrimSpace(keys[editKeyID(site, slug)]); k != "" {
				return k
			}
		}
	}
	if path, err := secretPath("cw-api-key"); err == nil {
		if raw, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(raw))
		}
	}
	return ""
}

func rememberEditKey(site, slug, key string) error {
	path, err := editKeyPath()
	if err != nil {
		return err
	}
	return rememberSecret(path, editKeyID(site, slug), key)
}

func rememberPassword(site, slug, password string) error {
	path, err := passwordPath()
	if err != nil {
		return err
	}
	return rememberSecret(path, editKeyID(site, slug), password)
}

// rememberSecret writes one value into one of the files in the secrets
// directory. They are all the same shape, site and name to the secret, and all
// of them are 0600 and nowhere near the walkthrough, which lives in a
// repository somebody will commit.
func rememberSecret(path, id, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	all := map[string]string{}
	_ = readJSONFile(path, &all)
	all[id] = value

	body, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

// ---------------------------------------------------------------- the note

func publishNotePath(file string) string { return file + ".published.json" }

func lastPublish(file string) (published, bool) {
	var p published
	if err := readJSONFile(publishNotePath(file), &p); err != nil {
		return published{}, false
	}
	return p, p.Slug != ""
}

func rememberPublish(file string, p published) {
	// Failing to write the note is not worth failing a publish that worked; the
	// next run just offers to make a new one.
	_ = writeJSONFile(publishNotePath(file), p)
}

// ---------------------------------------------------------------- git and gh

var prURLPattern = regexp.MustCompile(`github\.com/([^/]+/[^/]+)/pull/(\d+)`)

// fillSource completes what the author should not have to type: which commit
// the snippets were taken from, and which files the change touches. Anything
// already in the file wins, because the author may know better.
func fillSource(d *Doc, root string) {
	if d.Source == nil {
		d.Source = &Source{}
	}
	s := d.Source

	if m := prURLPattern.FindStringSubmatch(s.URL); m != nil {
		if s.Repo == "" {
			s.Repo = m[1]
		}
		if s.Provider == "" {
			s.Provider = "github"
		}
		if s.Kind == "" {
			s.Kind = "pull-request"
		}
		fillFromPR(s, root, m[1], m[2])
	}
	if root == "" {
		return
	}
	if s.Commit == "" {
		if out, err := runIn(root, "git", "rev-parse", "HEAD"); err == nil {
			s.Commit = strings.TrimSpace(out)
		}
	}
	if s.Head == "" {
		if out, err := runIn(root, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if b := strings.TrimSpace(out); b != "HEAD" {
				s.Head = b
			}
		}
	}
}

// fillFromPR asks gh for the things only the pull request knows. It is a best
// effort: without gh, or without access, the walkthrough still publishes and
// the page falls back to a permalink on the commit.
func fillFromPR(s *Source, root, repo, number string) {
	out, err := runIn(root, "gh", "pr", "view", number, "--repo", repo,
		"--json", "headRefOid,baseRefName,headRefName,state,files")
	if err != nil {
		return
	}
	var pr struct {
		HeadRefOid  string `json:"headRefOid"`
		BaseRefName string `json:"baseRefName"`
		HeadRefName string `json:"headRefName"`
		State       string `json:"state"`
		Files       []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if json.Unmarshal([]byte(out), &pr) != nil {
		return
	}
	if s.Commit == "" {
		s.Commit = pr.HeadRefOid
	}
	if s.Base == "" {
		s.Base = pr.BaseRefName
	}
	if s.Head == "" {
		s.Head = pr.HeadRefName
	}
	if s.State == "" && pr.State != "" {
		s.State = strings.ToLower(pr.State)
	}
	if len(s.ChangedFiles) == 0 {
		for _, f := range pr.Files {
			s.ChangedFiles = append(s.ChangedFiles, f.Path)
		}
	}
}

func commitOf(w *Walkthrough) string {
	if w.Source == nil {
		return ""
	}
	return w.Source.Commit
}

/*
fillSource2 is fillSource for cw/2, and it is a second function rather than the
same one with branches because almost every field has a different name and a
different shape. cw/2 says where something came from in URLs and revisions: a
repository is a link rather than an owner/name shorthand, and a comparison has
two commits rather than two branch names, so that it still means the same thing
after somebody force-pushes.
*/
func fillSource2(d *Doc2, root string) {
	if d.Source == nil {
		d.Source = &Source2{}
	}
	s := d.Source

	if m := prURLPattern.FindStringSubmatch(s.URL); m != nil {
		if s.RepositoryURL == "" {
			s.RepositoryURL = "https://github.com/" + m[1]
		}
		if s.Provider == "" {
			s.Provider = "github"
		}
		if s.Kind == "" {
			s.Kind = "pull-request"
		}
		if s.Identifier == "" {
			s.Identifier = m[2]
		}
		if s.Label == "" {
			s.Label = "PR #" + m[2]
		}
		fillFromPR2(s, root, m[1], m[2])
	}
	if root == "" {
		return
	}
	if s.Revision == "" {
		if out, err := runIn(root, "git", "rev-parse", "HEAD"); err == nil {
			s.Revision = strings.TrimSpace(out)
		}
	}
}

func fillFromPR2(s *Source2, root, repo, number string) {
	out, err := runIn(root, "gh", "pr", "view", number, "--repo", repo,
		"--json", "headRefOid,baseRefOid,state,files")
	if err != nil {
		return
	}
	var pr struct {
		HeadRefOid string `json:"headRefOid"`
		BaseRefOid string `json:"baseRefOid"`
		State      string `json:"state"`
		Files      []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if json.Unmarshal([]byte(out), &pr) != nil {
		return
	}
	if s.Revision == "" {
		s.Revision = pr.HeadRefOid
	}
	if s.Comparison == nil && pr.BaseRefOid != "" && pr.HeadRefOid != "" {
		s.Comparison = &Comparison{BaseRevision: pr.BaseRefOid, HeadRevision: pr.HeadRefOid}
	}
	if s.State == "" && pr.State != "" {
		s.State = strings.ToLower(pr.State)
	}
	if len(s.ChangedFiles) > 0 {
		return
	}
	// cw/2 wants to know what happened to each file, and gh does not say. git
	// does, so it is asked first; the list from gh is the fallback, and there
	// every file goes in the bucket the format keeps for "something else",
	// because guessing "modified" would read as a fact.
	if pr.BaseRefOid != "" && pr.HeadRefOid != "" {
		if changed := changedFiles2(root, pr.BaseRefOid, pr.HeadRefOid); len(changed) > 0 {
			s.ChangedFiles = changed
			return
		}
	}
	for _, f := range pr.Files {
		s.ChangedFiles = append(s.ChangedFiles, FileChange{File: f.Path, Status: "other"})
	}
}

var gitStatusWords = map[byte]string{
	'A': "added", 'M': "modified", 'D': "deleted",
	'R': "renamed", 'C': "copied", 'T': "type-changed",
}

func changedFiles2(root, base, head string) []FileChange {
	out, err := runIn(root, "git", "diff", "--name-status", "--find-renames", base+"..."+head)
	if err != nil {
		return nil
	}
	var files []FileChange
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		cols := strings.Split(strings.TrimSpace(line), "\t")
		if len(cols) < 2 || cols[0] == "" {
			continue
		}
		word, known := gitStatusWords[cols[0][0]]
		if !known {
			word = "other"
		}
		// A rename and a copy name both sides: the old path first, the new one
		// second, and it is the new one the walkthrough points at.
		if len(cols) >= 3 && (word == "renamed" || word == "copied") {
			files = append(files, FileChange{File: cols[2], PreviousFile: cols[1], Status: word})
			continue
		}
		files = append(files, FileChange{File: cols[1], Status: word})
	}
	return files
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// splitList takes a comma separated flag value and gives back its parts.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ---------------------------------------------------------------- the lock

/*
Publishing is open: anybody who can reach the site can put a walkthrough on it,
and anybody with the link can read one that is not locked. That is right for a
walkthrough of a public repository, where the code in it is already readable by
anyone, and wrong for every other one.

So the default follows the repository rather than the publisher's memory. Public
goes out open, everything else goes out with a password that is made here,
printed once and written down beside the edit key. Saying nothing is the common
case, and the common case should not be the one that leaks.
*/

// defaultLock is what happens on a first publish when nobody said anything
// about a password. It takes the answer rather than asking for it, so the rule
// can be read, and tested, without a forge.
func defaultLock(public bool, why string) (*string, string) {
	if public {
		return nil, why + ", so it went out without a password"
	}
	pw := newPassword()
	return &pw, why + ", so it went out with a password"
}

// repoOf is the owner and name to ask about: what the walkthrough says, and
// failing that what the checkout it was published from calls its origin.
func repoOf(view *Walkthrough, root string) string {
	if view.Source != nil && view.Source.Repo != "" {
		return strings.Trim(view.Source.Repo, "/")
	}
	if root == "" {
		return ""
	}
	out, err := runIn(root, "git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return repoFromRemote(out)
}

// passwordAlphabet leaves out the characters that get misheard on a call and
// mistyped from a screenshot: l and 1, o and 0. Thirty-two of them divides 256,
// so picking with a modulo is not weighted towards the front of the alphabet.
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// newPassword is one somebody has to be able to pass on out loud. Eighteen
// characters of this is ninety bits, which is far past what an unlock form
// needs and costs nothing to carry.
func newPassword() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		die("this machine has no randomness to make a password out of: %v", err)
	}
	for i, v := range b {
		b[i] = passwordAlphabet[int(v)%len(passwordAlphabet)]
	}
	return string(b)
}
