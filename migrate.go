package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

/*
cw/1 is the first named version of the walkthrough format. Before it there was
just "the file the server reads", with the pull request spread over four
top-level fields. Anything else that wants to read a walkthrough, an IDE plugin
or something that plays it out loud, needs those four in one place and needs to
know which version it is holding.

Everything here is mechanical and lossless. A file that has already been lifted
is left alone, so running it twice is the same as running it once.
*/

// migrateLegacy lifts a pre-cw/1 document in place and says whether it changed
// anything. It works on the generic map rather than on Doc, because it has to
// run before validation: the old field names are not in the schema any more.
func migrateLegacy(obj map[string]any) bool {
	if _, ok := obj["version"]; ok {
		return false
	}
	obj["version"] = FormatV1

	src, _ := obj["source"].(map[string]any)
	if src == nil {
		src = map[string]any{}
	}
	moved := false
	for _, k := range []string{"repo", "number", "state", "url"} {
		v, ok := obj[k]
		if !ok {
			continue
		}
		delete(obj, k)
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		if _, taken := src[k]; !taken {
			src[k] = v
		}
		moved = true
	}
	if moved {
		if _, ok := src["provider"]; !ok {
			if u, _ := src["url"].(string); strings.Contains(u, "github.com") {
				src["provider"] = "github"
			}
		}
		if _, ok := src["kind"]; !ok {
			if u, _ := src["url"].(string); strings.Contains(u, "/pull/") {
				src["kind"] = "pull-request"
			}
		}
		obj["source"] = src
	}
	return true
}

// ---------------------------------------------------------------- ids

var idStrip = regexp.MustCompile(`-{2,}`)

// slug turns a title into an id: lowercase, ascii, words joined by dashes. It is
// only ever a starting point, because two steps may well be called the same
// thing; EnsureIDs is what makes the result unique.
func slug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case unicode.IsSpace(r), r == '-', r == '_', r == '/', r == '.':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(idStrip.ReplaceAllString(b.String(), "-"), "-")
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	if out == "" {
		return "x"
	}
	return out
}

// EnsureIDs fills in every id that is missing and leaves every id that is
// there, because a reader's saved progress hangs off them. Uniqueness is per
// level: a step id only has to be unique inside its section.
func EnsureIDs(d *Doc) int {
	filled := 0
	take := func(seen map[string]bool, want string) string {
		id := want
		for n := 2; seen[id]; n++ {
			id = fmt.Sprintf("%s-%d", want, n)
		}
		seen[id] = true
		return id
	}
	parts := map[string]bool{}
	for pi := range d.Parts {
		p := &d.Parts[pi]
		if p.ID == "" {
			p.ID = take(parts, slug(p.Title))
			filled++
		} else {
			parts[p.ID] = true
		}
		sections := map[string]bool{}
		for si := range p.Sections {
			s := &p.Sections[si]
			if s.ID == "" {
				s.ID = take(sections, slug(s.Title))
				filled++
			} else {
				sections[s.ID] = true
			}
			steps := map[string]bool{}
			for ii := range s.Steps {
				st := &s.Steps[ii]
				if st.ID == "" {
					st.ID = take(steps, slug(st.Title))
					filled++
				} else {
					steps[st.ID] = true
				}
			}
		}
	}
	return filled
}

// ---------------------------------------------------------------- snippets

// normalizeSnippet is the one definition of what a snippet's text is, so the
// hash on this machine and the hash on someone else's agree: line endings
// folded, trailing blank lines dropped.
func normalizeSnippet(text string) string {
	return strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func snippetSha(text string) string {
	sum := sha256.Sum256([]byte(normalizeSnippet(text)))
	return hex.EncodeToString(sum[:])
}

func snippetLines(text string) int {
	return len(strings.Split(normalizeSnippet(text), "\n"))
}

// EnsureAnchors fills in to and sha for every snippet that has a place in a
// file. A snippet without a from is a shape or an example rather than a
// location, so it is left as it is.
func EnsureAnchors(d *Doc) int {
	filled := 0
	do := func(from int, to *int, sha *string, text string) {
		if *sha == "" {
			*sha = snippetSha(text)
			filled++
		}
		if from > 0 && *to == 0 {
			*to = from + snippetLines(text) - 1
			filled++
		}
	}
	for pi := range d.Parts {
		for si := range d.Parts[pi].Sections {
			for ii := range d.Parts[pi].Sections[si].Steps {
				st := &d.Parts[pi].Sections[si].Steps[ii]
				if st.Code != nil {
					do(st.Code.From, &st.Code.To, &st.Code.Sha, st.Code.Text)
				}
				if st.Diagram == nil {
					continue
				}
				for _, r := range st.Diagram.Refs {
					do(r.From, &r.To, &r.Sha, r.Code)
				}
			}
		}
	}
	return filled
}
