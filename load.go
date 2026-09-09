package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

/*
There are two versions of the format now, and one program that reads both. This
is the seam.

A document says which version it is, and that is what decides everything after:
which schema it is checked against, which struct it becomes, and which renderer
draws it. The $schema next to it is what an editor follows while somebody types;
it is checked for agreement and never believed over the version, because it can
be a relative path, a stale URL, or missing.

Everything downstream of here wants the same handful of facts from a
walkthrough, and Walkthrough is those facts. It is a struct rather than an
interface because the fields have the obvious names and a Doc already has fields
with those names, and one of the two would have had to be renamed for no reason.
*/

type LoadResult struct {
	Doc      *Doc
	Doc2     *Doc2
	Errors   []string
	Warnings []string
}

func LoadDoc(path string) (*LoadResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDoc(raw, path)
}

// ParseDoc is LoadDoc without the file, so the API validates the bytes a
// publisher posts through exactly the path cw check walks. The name is only
// used to say where a complaint came from.
func ParseDoc(raw []byte, name string) (*LoadResult, error) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", name, err)
	}

	res := &LoadResult{}
	// A file written before cw/1 is lifted first, so the validator sees the
	// shape it knows and the author gets errors about their walkthrough rather
	// than about a format they never chose.
	obj, _ := generic.(map[string]any)
	if obj != nil && migrateLegacy(obj) {
		res.Warnings = append(res.Warnings,
			"this file was written before cw/1 and was read as if it had been migrated. Run cw migrate to make that permanent")
	}

	version, _ := obj["version"].(string)
	sch := schemaFor(version)
	if sch == nil {
		return nil, fmt.Errorf("%s says its version is %q, and this build reads %s and %s. A reader that does not know a version refuses the file rather than guessing which half of it it still understands",
			name, version, FormatV1, FormatV2)
	}
	res.Errors = sch.Validate(generic)

	lifted, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("%s could not be re-read after migration: %w", name, err)
	}

	switch version {
	case FormatV1:
		d := &Doc{}
		if err := json.Unmarshal(lifted, d); err != nil {
			return nil, fmt.Errorf("%s does not match the schema: %w", name, err)
		}
		res.Doc = d
		inspect(res, d)
		res.checkSchemaURL(d.Schema, FormatV1)
	case FormatV2:
		d := &Doc2{}
		if err := json.Unmarshal(lifted, d); err != nil {
			return nil, fmt.Errorf("%s does not match the schema: %w", name, err)
		}
		res.Doc2 = d
		inspect2(res, d)
		res.checkSchemaURL(d.Schema, FormatV2)
	}
	return res, nil
}

// checkSchemaURL catches the file that was upgraded by hand and still points an
// editor at the old contract. Nothing breaks, but the author is being told the
// document is wrong by a tool that is reading the wrong rulebook.
func (r *LoadResult) checkSchemaURL(url, version string) {
	other := FormatV1
	if version == FormatV1 {
		other = FormatV2
	}
	if url != "" && strings.Contains(url, versionFile(other)) {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"$schema points at %s while version says %s, so your editor is checking this against the wrong one. Point it at %s",
			other, version, versionFile(version)))
	}
}

func versionFile(version string) string {
	if version == FormatV1 {
		return "v1.json"
	}
	return "v2.json"
}

// ---------------------------------------------------------------- one shape

// SourceView is where a walkthrough came from, in the one shape the link
// builder and the checkout finder read. The two versions spell it differently:
// cw/1 has repo and commit, cw/2 has repositoryUrl and revision and a comparison
// with two ends to it.
type SourceView struct {
	Kind         string
	Provider     string
	Repo         string
	Number       string
	URL          string
	State        string
	Commit       string
	Base         string
	Head         string
	ChangedFiles []string
}

// Verdict is what a working tree said about one snippet, and when, and against
// what. cw/1 keeps the first of those and cw/2 keeps all three.
type Verdict struct {
	Check    *Check
	At       time.Time
	Revision string
	Dirty    bool
}

// SnippetRef is a pasted piece of code with a place in a file, and a way back
// into the document to say what became of it. It is what lets one comparison
// against a working tree serve both versions.
type SnippetRef struct {
	File string
	From int
	Text string
	Set  func(Verdict)
}

// Walkthrough is a loaded document of either version, in the shape the server,
// the publisher and the reader all want.
type Walkthrough struct {
	Format   string
	Title    string
	Summary  string
	RootHint string
	Source   *SourceView
	Steps    int
	Files    []string
	Snippets []SnippetRef

	// Exactly one of these is set, for the few places that genuinely have to
	// know which version they are holding.
	V1 *Doc
	V2 *Doc2
}

func (r *LoadResult) View() *Walkthrough {
	switch {
	case r == nil:
		return nil
	case r.Doc != nil:
		return viewOf1(r.Doc)
	case r.Doc2 != nil:
		return viewOf2(r.Doc2)
	}
	return nil
}

// Raw is the document as it would be written to disk, which is what the page
// gets and what the store keeps.
func (w *Walkthrough) Raw() ([]byte, error) {
	switch {
	case w == nil:
		return nil, fmt.Errorf("no walkthrough")
	case w.V1 != nil:
		return json.Marshal(w.V1)
	case w.V2 != nil:
		return json.Marshal(w.V2)
	}
	return nil, fmt.Errorf("a walkthrough of no version")
}

func viewOf1(d *Doc) *Walkthrough {
	w := &Walkthrough{
		Format: FormatV1, Title: d.Title, Summary: d.Summary, RootHint: d.Root,
		Steps: d.Steps(), Files: d.Files(), V1: d,
	}
	if s := d.Source; s != nil {
		w.Source = &SourceView{
			Kind: s.Kind, Provider: s.Provider, Repo: s.Repo, Number: s.Number,
			URL: s.URL, State: s.State, Commit: s.Commit, Base: s.Base, Head: s.Head,
			ChangedFiles: s.ChangedFiles,
		}
	}
	for pi := range d.Parts {
		for si := range d.Parts[pi].Sections {
			for ii := range d.Parts[pi].Sections[si].Steps {
				st := &d.Parts[pi].Sections[si].Steps[ii]
				if c := st.Code; c != nil {
					w.Snippets = append(w.Snippets, SnippetRef{
						File: c.File, From: c.From, Text: c.Text,
						Set: func(v Verdict) { c.Check = v.Check },
					})
				}
				if st.Diagram == nil {
					continue
				}
				for _, ref := range st.Diagram.Refs {
					w.Snippets = append(w.Snippets, SnippetRef{
						File: ref.File, From: ref.From, Text: ref.Code,
						Set: func(v Verdict) { ref.Check = v.Check },
					})
				}
			}
		}
	}
	return w
}

func viewOf2(d *Doc2) *Walkthrough {
	w := &Walkthrough{
		Format: FormatV2, Title: d.Title, Summary: d.Summary,
		Steps: d.Steps(), Files: d.Files(), Source: sourceViewOf2(d.Source), V2: d,
	}
	// cw/2 has no root: which checkout the paths hang off is a fact about the
	// machine reading it, not about the document, so it stays with the reader.
	d.walkBlocks(func(at string, b *Block) {
		if b.Type != BlockCode || b.Snippet == nil || b.Snippet.Source == nil {
			return
		}
		s := b.Snippet
		w.Snippets = append(w.Snippets, SnippetRef{
			File: s.Source.File, From: s.Source.StartLine, Text: s.Text,
			Set: func(v Verdict) { s.Verification = verificationOf(v, s.Source) },
		})
	})
	return w
}

func sourceViewOf2(s *Source2) *SourceView {
	if s == nil {
		return nil
	}
	v := &SourceView{
		Kind: s.Kind, Provider: s.Provider, URL: s.URL, State: s.State,
		Repo: repoFromURL(s.RepositoryURL), Commit: s.Revision,
		Number: firstOf(s.Label, s.Identifier),
	}
	if c := s.Comparison; c != nil {
		v.Base, v.Head = c.BaseRevision, c.HeadRevision
		if v.Commit == "" {
			v.Commit = c.HeadRevision
		}
	}
	// cw/1 asked an author to write the provider down. cw/2 has the URL, and the
	// host of a URL is not a matter of opinion.
	if v.Provider == "" && strings.Contains(s.RepositoryURL+s.URL, "github.com/") {
		v.Provider = "github"
	}
	for _, f := range s.ChangedFiles {
		v.ChangedFiles = append(v.ChangedFiles, f.File)
	}
	return v
}

// repoFromURL pulls the owner/name a link builder needs back out of a full
// repository URL, which is the form cw/2 keeps because not every forge has an
// owner and a name.
func repoFromURL(u string) string {
	if u == "" {
		return ""
	}
	rest := u
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[i+1:]
	} else {
		return ""
	}
	rest = strings.TrimSuffix(strings.Trim(rest, "/"), ".git")
	if strings.Count(rest, "/") != 1 {
		return ""
	}
	return rest
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// verificationOf says what the tree found, in the words cw/2 uses. With no
// revision to name there is nothing honest to write, and an absent verification
// already means the snippet was never checked.
func verificationOf(v Verdict, src *Location) *Verification {
	if v.Check == nil || v.Check.State == "unchecked" || v.Revision == "" {
		return nil
	}
	dirty := v.Dirty
	out := &Verification{
		CheckedAt: v.At.UTC().Format(time.RFC3339),
		Against:   Against{Kind: "working-tree", Revision: v.Revision, Dirty: &dirty},
		Note:      v.Check.Note,
	}
	switch v.Check.State {
	case "ok":
		out.State = "match"
	case "moved":
		if v.Check.Line <= 0 {
			// Moved with nowhere to move to is not a place, it is a difference.
			out.State = "different"
			break
		}
		out.State = "moved"
		out.ResolvedSource = &Location{File: src.File, StartLine: v.Check.Line}
	case "gone":
		out.State = "different"
	case "missing-file":
		out.State = "missing-file"
	default:
		out.State = "unavailable"
	}
	return out
}

// ---------------------------------------------------------------- counting

func (d *Doc2) Steps() int {
	n := 0
	for _, p := range d.Parts {
		for _, s := range p.Sections {
			n += len(s.Steps)
		}
	}
	return n
}

// Files lists every path the walkthrough points at, once, in reading order.
func (d *Doc2) Files() []string {
	var out []string
	seen := map[string]bool{}
	add := func(f string) {
		if f == "" || seen[f] {
			return
		}
		seen[f] = true
		out = append(out, f)
	}
	d.walkBlocks(func(at string, b *Block) {
		switch b.Type {
		case BlockCode:
			if b.Snippet != nil && b.Snippet.Source != nil {
				add(b.Snippet.Source.File)
			}
		case BlockDiff:
			for _, loc := range []*Location{b.After, b.Before} {
				if loc != nil {
					add(loc.File)
				}
			}
		}
	})
	return out
}

// ---------------------------------------------------------------- printing

/*
Tour is a walkthrough in the shape cw check prints it: the parts, the sections
under them, and the snippets each section points at, with whatever the working
tree said about them.

It is built after Verify rather than during it, so the states it reads are the
ones just written. Nothing but the command wants a document in this shape, and
both versions can answer it, so it is here instead of twice in the command.
*/

type TourPart struct {
	Title    string
	Sections []TourSection
}

type TourSection struct {
	Title    string
	Snippets []TourSnippet
}

// TourSnippet says the state in the document's own words. cw/1 and cw/2 do not
// use the same ones, and translating between them would mean inventing a third
// set that neither format has ever heard of.
type TourSnippet struct {
	Name  string
	State string
	Note  string
}

func (w *Walkthrough) Tour() []TourPart {
	switch {
	case w == nil:
		return nil
	case w.V1 != nil:
		return tourOf1(w.V1)
	case w.V2 != nil:
		return tourOf2(w.V2)
	}
	return nil
}

func tourOf1(d *Doc) []TourPart {
	out := make([]TourPart, 0, len(d.Parts))
	for _, p := range d.Parts {
		tp := TourPart{Title: p.Title}
		for _, s := range p.Sections {
			ts := TourSection{Title: s.Title}
			for _, st := range s.Steps {
				if c := st.Code; c != nil {
					ts.Snippets = append(ts.Snippets, snippetOf1(c.File, c.Check))
				}
				if st.Diagram == nil {
					continue
				}
				for key, r := range st.Diagram.Refs {
					ts.Snippets = append(ts.Snippets, snippetOf1(r.File+" ("+key+")", r.Check))
				}
			}
			tp.Sections = append(tp.Sections, ts)
		}
		out = append(out, tp)
	}
	return out
}

func snippetOf1(name string, c *Check) TourSnippet {
	if c == nil {
		return TourSnippet{Name: name}
	}
	return TourSnippet{Name: name, State: c.State, Note: c.Note}
}

func tourOf2(d *Doc2) []TourPart {
	out := make([]TourPart, 0, len(d.Parts))
	for _, p := range d.Parts {
		tp := TourPart{Title: p.Title}
		for _, s := range p.Sections {
			ts := TourSection{Title: s.Title}
			for _, st := range s.Steps {
				for bi := range st.Blocks {
					b := &st.Blocks[bi]
					if b.Type != BlockCode || b.Snippet == nil || b.Snippet.Source == nil {
						continue
					}
					snip := TourSnippet{Name: b.Snippet.Source.File}
					if v := b.Snippet.Verification; v != nil {
						snip.State, snip.Note = v.State, v.Note
					}
					ts.Snippets = append(ts.Snippets, snip)
				}
			}
			tp.Sections = append(tp.Sections, ts)
		}
		out = append(out, tp)
	}
	return out
}

// Document is the concrete document behind the view, for the two places that
// write one back out: cw migrate and cw open.
func (w *Walkthrough) Document() any {
	switch {
	case w == nil:
		return nil
	case w.V1 != nil:
		return w.V1
	case w.V2 != nil:
		return w.V2
	}
	return nil
}
