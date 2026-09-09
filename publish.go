package main

import (
	"bytes"
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

	res, err := LoadDoc(f.file, mustSchema())
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
	d := res.Doc

	// The tree check is the whole reason to publish from here rather than by
	// posting the file from anywhere.
	root := resolveRoot(flags{file: f.file, root: f.root}, d)
	tree := &Tree{Root: root}
	checked, moved, stale := tree.Verify(d)
	if root == "" {
		fmt.Fprintf(os.Stderr, "cw: no working tree was found, so the snippets were not checked. "+
			"The page will say the code is unverified.\n")
	}
	if stale > 0 && !f.force {
		fmt.Fprintf(os.Stderr, "cw: %d snippet(s) are no longer in the working tree. Run cw check to see which, "+
			"fix them, or publish with --force to say it out loud on the page.\n", stale)
		os.Exit(1)
	}

	fillSource(d, root)
	EnsureIDs(d)
	EnsureAnchors(d)

	body, err := json.Marshal(d)
	if err != nil {
		die("%v", err)
	}

	target, method, slug := f.resolveTarget()
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

	fmt.Printf("%s\n", out.URL)
	fmt.Printf("  %s\n", d.Title)
	if root != "" {
		fmt.Printf("  %d snippet(s) checked: %d moved, %d no longer there\n", checked, moved, stale)
	}
	if c := commitOf(d); c != "" {
		fmt.Printf("  against commit %s\n", short(c))
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

func editKeyPath() (string, error) { return secretPath("cw-keys.json") }

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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	keys := map[string]string{}
	_ = readJSONFile(path, &keys)
	keys[editKeyID(site, slug)] = key

	body, err := json.MarshalIndent(keys, "", "  ")
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

func commitOf(d *Doc) string {
	if d.Source == nil {
		return ""
	}
	return d.Source.Commit
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
