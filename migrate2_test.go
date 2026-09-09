package main

import (
	"strings"
	"testing"
)

// oneOfEach is a cw/1 document holding every case the lift cannot finish on its
// own, so the test is about what it refuses to invent rather than what it copies.
func oneOfEach() *Doc {
	return &Doc{
		Version: FormatV1, Title: "The old one", Root: "../..",
		Source: &Source{
			Kind: "pull-request", Provider: "github", Repo: "o/r", Number: "PR #7",
			URL: "https://github.com/o/r/pull/7", Commit: "9f2c1ab",
			Base: "main", Head: "feature/x",
			ChangedFiles: []string{"a.ts", "b.ts"},
		},
		Parts: []Part{{
			ID: "shared", Title: "One", Desc: "short", Long: "longer",
			Sections: []Section{{
				Title: "Two", Desc: "about",
				Steps: []Step{
					{
						// The same id as the part, which cw/1 allowed because it
						// gave every level its own namespace.
						ID: "shared", Title: "Code", Body: "Prose.", Callout: "Careful.",
						Code: &Code{
							File: "a.ts", From: 40, To: 43, Sha: "whatever", Lang: "typescript",
							Text:  "one\ntwo\nthree\nfour\n",
							Hi:    []int{41, 42, 43},
							Add:   []int{43},
							Notes: []LineNote{{Line: 42, Text: "About the second line."}},
							Check: &Check{State: "ok", Line: 40},
						},
					},
					{
						Title: "Picture", Body: "Prose.",
						Diagram: &Diagram{
							Kind: "flow", Def: "flowchart LR\n  api[API]",
							Refs: map[string]*Ref{"api": {
								Label: "the api", File: "b.ts", From: 3,
								Note: "Why this block is worth opening.",
								Code: "export const api = 1;",
							}},
						},
					},
					{
						Title: "Change", Body: "Prose.",
						Diff: &Diff{File: "c.ts", From: 12, Lines: []DiffLine{
							{Kind: "ctx", T: "kept"},
							{Kind: "del", T: "was"},
							{Kind: "add", T: "is"},
						}},
					},
					{
						Title: "Over time", Body: "Prose.",
						Anim: &Anim{Frames: []Frame{
							{Label: "one", Nodes: []FrameNode{
								{Label: "key A", Sub: "active", State: "active"},
								{Label: "key B", State: "spinning"},
							}},
							{Label: "two", Nodes: []FrameNode{
								{Label: "key A", State: "done"},
								{Label: "key B", State: ""},
							}},
						}},
					},
				},
			}},
		}},
	}
}

func liftText(t *testing.T, d *Doc, opt LiftOptions) (*Doc2, string) {
	t.Helper()
	out, todo := LiftToV2(d, opt)
	EnsureAnchors2(out)
	return out, strings.Join(todo, "\n")
}

func blockAt(t *testing.T, d *Doc2, step, i int) *Block {
	t.Helper()
	blocks := d.Parts[0].Sections[0].Steps[step].Blocks
	if i >= len(blocks) {
		t.Fatalf("step %d has %d blocks, wanted [%d]", step, len(blocks), i)
	}
	return &blocks[i]
}

// The lift is only worth having if what it does produce is right, so this is the
// half that comes across without a question.
func TestTheLiftCarriesOverWhatItCan(t *testing.T) {
	out, _ := liftText(t, oneOfEach(), LiftOptions{})

	if out.Version != FormatV2 || out.Title != "The old one" {
		t.Errorf("the head did not come across: %+v", out)
	}
	if out.Source.RepositoryURL != "https://github.com/o/r" || out.Source.Revision != "9f2c1ab" {
		t.Errorf("source: %+v", out.Source)
	}
	if out.Source.Identifier != "7" || out.Source.Label != "PR #7" {
		t.Errorf("the pull request lost its number: %+v", out.Source)
	}
	p := out.Parts[0]
	if p.Summary != "short" || p.Description != "longer" || p.Sections[0].Summary != "about" {
		t.Errorf("desc and long did not become summary and description: %+v", p)
	}
	// Prose, then the code, then the callout: the order cw/1 rendered in.
	kinds := []string{}
	for _, b := range p.Sections[0].Steps[0].Blocks {
		kinds = append(kinds, b.Type)
	}
	if strings.Join(kinds, ",") != "markdown,code,callout" {
		t.Errorf("the blocks came out as %v", kinds)
	}
	if b := blockAt(t, out, 0, 2); b.Severity != "warning" {
		t.Errorf("a cw/1 callout is a warning, got %q", b.Severity)
	}
}

// The one thing most likely to go wrong: cw/1 counted from the file and cw/2
// counts from the snippet.
func TestLineNumbersBecomeSnippetRelative(t *testing.T) {
	out, _ := liftText(t, oneOfEach(), LiftOptions{})
	s := blockAt(t, out, 0, 1).Snippet

	if s.Source.StartLine != 40 || s.Source.EndLine != 43 {
		t.Errorf("source range is %+v, want 40 to 43", s.Source)
	}
	// hi was 41, 42, 43: three lines in a row, so one range of 2 to 4.
	if len(s.Highlights) != 2 {
		t.Fatalf("highlights: %+v", s.Highlights)
	}
	if h := s.Highlights[0]; h.Lines.Start != 2 || h.Lines.End != 4 || h.Kind != "" {
		t.Errorf("the focus range is %+v, want 2 to 4", h)
	}
	if h := s.Highlights[1]; h.Lines.Start != 4 || h.Kind != "added" {
		t.Errorf("the added range is %+v, want line 4", h)
	}
	if a := s.Annotations[0]; a.Lines.Start != 3 {
		t.Errorf("the annotation is on %+v, want line 3", a.Lines)
	}
}

// The hashing rule changed, so carrying the old value over would be carrying
// over a wrong answer that looks like a right one.
func TestTheHashIsRecomputedRatherThanCopied(t *testing.T) {
	out, _ := liftText(t, oneOfEach(), LiftOptions{})
	s := blockAt(t, out, 0, 1).Snippet
	if s.Hash == nil || s.Hash.Value == "whatever" {
		t.Fatalf("the hash was carried over: %+v", s.Hash)
	}
	if s.Hash.Value != snippetHash2(s.Text) {
		t.Error("the hash does not match the text it was computed from")
	}
	if s.Hash.Value == snippetSha(s.Text) {
		t.Error("the hash was computed by cw/1's rule, which trims the trailing newline")
	}
}

func TestTheDocumentWideNamespaceIsMadeToFit(t *testing.T) {
	out, todo := liftText(t, oneOfEach(), LiftOptions{})
	if out.Parts[0].ID != "shared" {
		t.Errorf("the first thing to claim an id should keep it, got %q", out.Parts[0].ID)
	}
	if id := out.Parts[0].Sections[0].Steps[0].ID; id != "shared-2" {
		t.Errorf("the second should have been renamed, got %q", id)
	}
	if !strings.Contains(todo, `the id "shared" was already taken`) {
		t.Errorf("the rename was not reported:\n%s", todo)
	}
}

// And now the seven it refuses to answer.
func TestTheLiftInventsNothing(t *testing.T) {
	out, todo := liftText(t, oneOfEach(), LiftOptions{})

	if !strings.Contains(todo, "root is not a field any more") {
		t.Error("root going away was not reported")
	}

	// 1. A diagram needs alt text, and alt text is a sentence somebody writes.
	dg := blockAt(t, out, 1, 1)
	if dg.Type != "diagram" || dg.Alt != "" {
		t.Errorf("the diagram came out as %+v, and alt should be empty", dg)
	}
	if !strings.Contains(todo, "needs an alt") {
		t.Error("the missing alt was not reported")
	}

	// 2. A cw/1 diff has one starting line and cw/2 needs one per side.
	df := blockAt(t, out, 2, 1)
	if df.Type != "code" {
		t.Errorf("without --assume-diff-start a diff stays a snippet, got %q", df.Type)
	}
	if df.Snippet.Language != "diff" || !strings.Contains(df.Snippet.Text, "-was") {
		t.Errorf("the diff lines were not kept: %+v", df.Snippet)
	}
	if !strings.Contains(todo, "kept as a snippet rather than a diff") {
		t.Error("the diff was not reported")
	}

	// 3. A check has no date and no revision in cw/1, so there is no honest
	// verification to write.
	if v := blockAt(t, out, 0, 1).Snippet.Verification; v != nil {
		t.Errorf("a verification was invented: %+v", v)
	}
	if !strings.Contains(todo, "snippet check(s) were dropped") {
		t.Error("the dropped check was not reported")
	}

	// 4 and 5. Branch names are not revisions, and a changed file with no status
	// is not a changed file cw/2 can hold.
	if out.Source.Comparison != nil {
		t.Errorf("a comparison was invented out of branch names: %+v", out.Source.Comparison)
	}
	if len(out.Source.ChangedFiles) != 0 {
		t.Errorf("changed files were invented a status: %+v", out.Source.ChangedFiles)
	}
	for _, want := range []string{"branch names", "changedFiles were dropped"} {
		if !strings.Contains(todo, want) {
			t.Errorf("%q was not reported:\n%s", want, todo)
		}
	}

	// 6. A cw/2 snippet has a label but no note, so the sentence has to go
	// somewhere and where is a reading decision.
	if b := blockAt(t, out, 1, 3); b.Type != "markdown" || !strings.Contains(b.Text, "worth opening") {
		t.Errorf("the ref note did not become a paragraph: %+v", b)
	}
	if !strings.Contains(todo, "had a note, which became a paragraph") {
		t.Error("the moved note was not reported")
	}

	// 7. A state cw/2 has never heard of is passed through, so the schema
	// refuses it by name instead of this quietly turning it into idle.
	tl := blockAt(t, out, 3, 1)
	if tl.Type != "timeline" {
		t.Fatalf("anim became %q", tl.Type)
	}
	if s := tl.Frames[0].States[1].State; s != "spinning" {
		t.Errorf("the unknown state became %q instead of being left alone", s)
	}
	if s := tl.Frames[1].States[1].State; s != "idle" {
		t.Errorf("an empty state means idle, which the page already did, got %q", s)
	}
	if !strings.Contains(todo, `is in state "spinning"`) {
		t.Error("the unknown state was not reported")
	}
}

// A diagram ref carried its own copy of the code, and in cw/2 it is a code block
// the diagram links to by name.
func TestARefBecomesABlockTheDiagramPointsAt(t *testing.T) {
	out, _ := liftText(t, oneOfEach(), LiftOptions{})
	dg := blockAt(t, out, 1, 1)
	code := blockAt(t, out, 1, 2)

	if len(dg.Links) != 1 || dg.Links[0].NodeID != "api" {
		t.Fatalf("links: %+v", dg.Links)
	}
	if code.Type != "code" || code.ID != dg.Links[0].BlockID {
		t.Errorf("the link points at %q and the block is %q", dg.Links[0].BlockID, code.ID)
	}
	if code.Snippet.Source.File != "b.ts" || code.Snippet.Source.StartLine != 3 {
		t.Errorf("the snippet lost its place: %+v", code.Snippet.Source)
	}
}

// Taking the old number for both sides is a decision, so it is a flag, and it
// still says out loud that it was assumed.
func TestAssumeDiffStartIsOptedInToAndSaidOutLoud(t *testing.T) {
	out, todo := liftText(t, oneOfEach(), LiftOptions{AssumeDiffStart: true})
	df := blockAt(t, out, 2, 1)
	if df.Type != "diff" {
		t.Fatalf("with the flag it should be a diff, got %q", df.Type)
	}
	h := df.Hunks[0]
	if h.OldStart != 12 || h.NewStart != 12 || h.OldLines != 2 || h.NewLines != 2 {
		t.Errorf("hunk: %+v", h)
	}
	if !strings.Contains(todo, "Check it against the real diff") {
		t.Errorf("the assumption was not reported:\n%s", todo)
	}
}

// The whole point of a lift is that what comes out is a document, so it goes
// through the same door every other document does.
func TestWhatComesOutIsReadAsCw2(t *testing.T) {
	out, _ := liftText(t, oneOfEach(), LiftOptions{})
	// The two things the lift left for a person, done the way a person would.
	out.Parts[0].Sections[0].Steps[1].Blocks[1].Alt = "A picture of the api."
	out.Parts[0].Sections[0].Steps[3].Blocks[1].Frames[0].States[1].State = "idle"

	raw, err := marshalPlain(out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseDoc(raw, "lifted.json")
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc2 == nil {
		t.Fatal("it did not come back as cw/2")
	}
	if len(res.Errors) > 0 {
		t.Errorf("with the alt written in it should be clean:\n%s", strings.Join(res.Errors, "\n"))
	}
}

// A title longer than an id can hold is cut, and a cut id is a renamed id. The
// peer session that migrated sixteen walkthroughs hit one that landed exactly on
// the limit, and nothing said so.
func TestATruncatedIdSaysSoOutLoud(t *testing.T) {
	long := "And the environment provider is kept, not replaced, when the host restarts"
	d := &Doc{Version: FormatV1, Title: "t", Parts: []Part{{
		Title: "One", Sections: []Section{{
			Title: "Two", Steps: []Step{{Title: long, Body: "Prose."}},
		}},
	}}}

	out, todo := LiftToV2(d, LiftOptions{})
	id := out.Parts[0].Sections[0].Steps[0].ID
	if len(id) != idLimit {
		t.Fatalf("the id is %q, %d characters, and the limit is %d", id, len(id), idLimit)
	}
	if !strings.Contains(strings.Join(todo, "\n"), "cut to 48 characters") {
		t.Errorf("the cut was silent:\n%s", strings.Join(todo, "\n"))
	}

	// A title that fits says nothing, because there is nothing to check.
	short := &Doc{Version: FormatV1, Title: "t", Parts: []Part{{
		Title: "One", Sections: []Section{{
			Title: "Two", Steps: []Step{{Title: "A short one", Body: "Prose."}},
		}},
	}}}
	if _, quiet := LiftToV2(short, LiftOptions{}); strings.Contains(strings.Join(quiet, "\n"), "cut to") {
		t.Errorf("a title that fits was reported anyway:\n%s", strings.Join(quiet, "\n"))
	}
}
