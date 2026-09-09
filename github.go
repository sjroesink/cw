package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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
func BuildGitHubLinks(src *Source) *GitHubLinks {
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
// sends a reader to a page that does not exist.
func prFilesURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" || !strings.Contains(u, "github.com/") {
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
