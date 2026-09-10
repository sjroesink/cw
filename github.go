package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

/*
On the hosted page there is no editor to jump into, so a line number points at
GitHub instead. Two shapes, and which one is right depends on whether the file
is part of the change:

  a file the pull request touches   the line in the diff, where the reader sees
                                    what changed rather than only the result
  anything else                     a permalink on the commit, which stays
                                    correct after the branch moves on

The anchor GitHub uses for a file in a diff is "diff-" followed by the sha256 of
the file path. That is not documented anywhere, so it is worked out here where a
test can hold it, and the permalink is the fallback whenever there is no pull
request or no commit to hang one off.
*/

// GitHubLinks is everything the page needs to turn a path and a line into a
// URL, worked out once per walkthrough. The page does no hashing of its own.
type GitHubLinks struct {
	Repo     string            `json:"repo,omitempty"`
	Commit   string            `json:"commit,omitempty"`
	BlobBase string            `json:"blobBase,omitempty"`
	PRFiles  string            `json:"prFiles,omitempty"`
	Anchors  map[string]string `json:"anchors,omitempty"`
}

// BuildGitHubLinks returns nil when there is nothing to link to, so the page can
// treat "no links" as one case rather than as an empty object with empty fields.
func BuildGitHubLinks(src *SourceView) *GitHubLinks {
	if src == nil {
		return nil
	}
	if src.Provider != "" && src.Provider != "github" {
		return nil
	}
	repo := strings.Trim(strings.TrimSpace(src.Repo), "/")
	out := &GitHubLinks{Repo: repo, Commit: src.Commit}

	if repo != "" && strings.Count(repo, "/") == 1 && src.Commit != "" {
		out.BlobBase = "https://github.com/" + repo + "/blob/" + src.Commit + "/"
	}
	if files := prFilesURL(src.URL); files != "" {
		out.PRFiles = files
		out.Anchors = map[string]string{}
		for _, f := range src.ChangedFiles {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			out.Anchors[f] = diffAnchor(f)
		}
	}
	if out.BlobBase == "" && out.PRFiles == "" {
		return nil
	}
	return out
}

// diffAnchor is the id GitHub gives a file in a diff: the sha256 of the path it
// has on the new side, hex, with a diff- in front. A line is addressed by
// appending R and the line number for the new side, L for the old.
func diffAnchor(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "diff-" + hex.EncodeToString(sum[:])
}

// prFilesURL turns a pull request URL into its file view, and returns empty for
// anything that is not one. It is deliberately strict: guessing wrong here
// sends a reader to a page that does not exist. The host is part of that, and
// it has to be github.com itself rather than a URL with those characters
// somewhere in its path, because what comes out of here is what the page opens.
func prFilesURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if !strings.HasPrefix(u, "https://github.com/") {
		return ""
	}
	i := strings.Index(u, "/pull/")
	if i < 0 {
		return ""
	}
	number := u[i+len("/pull/"):]
	if number == "" {
		return ""
	}
	// Anything after the number, /files or /commits, is replaced rather than
	// appended to.
	if cut := strings.IndexByte(number, '/'); cut >= 0 {
		number = number[:cut]
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return u[:i] + "/pull/" + number + "/files"
}

// ---------------------------------------------------------------- asking gh

// ghJSON runs gh and reads its answer. It gets a deadline of its own because it
// is a network call in the middle of opening a page: a forge that is slow today
// should cost a couple of seconds rather than the whole command.
func ghJSON(dir string, into any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("gh did not answer in time")
		}
		if msg := lastLine(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return json.Unmarshal(out, into)
}

// RepoIsPublic asks GitHub whether anybody can read a repository, and says how
// it knows.
//
// This decides whether a walkthrough goes out with a password on it, so what
// happens when there is no answer matters more than the answer itself. Not
// knowing counts as not public. A public repository that ends up locked costs
// somebody one flag; a private one that ends up open cannot be taken back,
// because whoever read it has read it.
func RepoIsPublic(repo, dir string) (bool, string) {
	if repo == "" {
		return false, "this walkthrough names no repository to ask about"
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return false, "gh is not on this machine, so whether " + repo + " is public could not be asked"
	}
	var out struct {
		Visibility string `json:"visibility"`
	}
	if err := ghJSON(dir, &out, "repo", "view", repo, "--json", "visibility"); err != nil {
		return false, "gh could not say whether " + repo + " is public: " + ghSaid(err)
	}
	switch v := strings.ToLower(out.Visibility); v {
	case "public":
		return true, repo + " is public"
	case "":
		return false, "gh named no visibility for " + repo
	default:
		return false, repo + " is " + v
	}
}

// repoFromRemote is repoFromURL over the other shape a remote comes in:
// git@github.com:owner/name.git, which has no scheme and a colon where the path
// starts.
func repoFromRemote(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "@"); i >= 0 && !strings.Contains(u, "://") {
		u = "ssh://" + strings.Replace(u[i+1:], ":", "/", 1)
	}
	return repoFromURL(u)
}

// ghSaid is the part of a gh error worth repeating. It puts its transport in
// front of the message and a category in brackets behind it, and neither says
// anything to somebody who just wanted to publish a walkthrough.
func ghSaid(err error) string {
	msg := strings.TrimPrefix(err.Error(), "GraphQL: ")
	if i := strings.Index(msg, ". ("); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSuffix(msg, ".")
}
