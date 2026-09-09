package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

/*
Lifting a cw/1 document to cw/2 is mechanical for most of it and impossible for
some of it, and the difference is the whole design of this file.

migrateLegacy, next door, is lossless: it renames fields that were renamed. This
is not. cw/2 asks for things cw/1 never recorded, and the answer to those is not
a good guess, it is a question for whoever wrote the walkthrough. So: do what is
certain, leave out what is not, and hand back a numbered list of every place
somebody has to look. A made-up line number is worse than a missing one, because
it looks exactly like a real one.

Seven kinds of gap, and they are all in the list this returns.
*/

type LiftOptions struct {
	// AssumeDiffStart takes a cw/1 diff at its word: one starting line meant for
	// both sides at once. That is true of the first hunk of a file and rarely
	// anywhere else, so it is off unless somebody decides otherwise.
	AssumeDiffStart bool
}

// LiftToV2 converts a loaded cw/1 document and says what it could not do.
func LiftToV2(d *Doc, opt LiftOptions) (*Doc2, []string) {
	var todo []string
	note := func(f string, a ...any) { todo = append(todo, fmt.Sprintf(f, a...)) }

	out := &Doc2{
		Version: FormatV2, Title: d.Title, Summary: d.Summary, Language: d.Language,
		Source: liftSource(d.Source, note),
		Ext:    liftExt(d.Ext),
	}
	if d.Root != "" {
		note("root is not a field any more: which checkout the paths hang off is a fact about the machine reading it. Pass --root to cw serve instead")
	}

	ids := &idSpace{taken: map[string]bool{}}
	dropped := 0

	for pi := range d.Parts {
		p := &d.Parts[pi]
		at := fmt.Sprintf("parts[%d] (%s)", pi, p.Title)
		np := Part2{
			ID: ids.claim(p.ID, p.Title, at, note), Title: p.Title,
			Summary: p.Desc, Description: p.Long, Files: p.Files, Ext: liftExt(p.Ext),
		}
		for si := range p.Sections {
			s := &p.Sections[si]
			sat := fmt.Sprintf("%s.sections[%d] (%s)", at, si, s.Title)
			ns := Section2{
				ID: ids.claim(s.ID, s.Title, sat, note), Title: s.Title,
				Summary: s.Desc, Ext: liftExt(s.Ext),
			}
			for ii := range s.Steps {
				st := &s.Steps[ii]
				iat := fmt.Sprintf("%s.steps[%d] (%s)", sat, ii, st.Title)
				nst := Step2{
					ID: ids.claim(st.ID, st.Title, iat, note), Title: st.Title,
					Speech: st.Speech, Ext: liftExt(st.Ext),
					Blocks: liftStep(st, ids, iat, opt, note, &dropped),
				}
				ns.Steps = append(ns.Steps, nst)
			}
			np.Sections = append(np.Sections, ns)
		}
		out.Parts = append(out.Parts, np)
	}

	if dropped > 0 {
		note("%d snippet check(s) were dropped: cw/2 records when a check was made and against which revision, and cw/1 recorded neither. The next cw publish against a working tree fills them in", dropped)
	}
	return out, todo
}

/* ------------------------------------------------------------------ ids */

// cw/1 gave each level its own namespace and cw/2 has one for the document, so a
// part and a step may have been called the same thing quite legally. The first
// one keeps the name, because a reader's saved place hangs off it.
type idSpace struct{ taken map[string]bool }

func (s *idSpace) claim(want, title, at string, note func(string, ...any)) string {
	base := want
	if base == "" {
		base = slug(title)
		// A long title is cut to fit, and a cut id is a renamed id: if this
		// walkthrough is already published, that step loses everybody's saved
		// place. Silently would be the wrong way to do that.
		if len(slugFull(title)) > idLimit {
			note("%s: the id was derived from the title and cut to %d characters, so it reads %q. If this walkthrough is already published, that step loses the progress readers had on it", at, idLimit, base)
		}
	}
	if base == "" {
		base = "item"
	}
	id := base
	for n := 2; s.taken[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	if want != "" && id != want {
		note("%s: the id %q was already taken elsewhere in the document, so this one became %q. cw/2 has one namespace for the whole document", at, want, id)
	}
	s.taken[id] = true
	return id
}

func liftExt(in map[string]any) map[string]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for k, v := range in {
		if raw, err := marshalPlain(v); err == nil {
			out[k] = raw
		}
	}
	return out
}

/* ------------------------------------------------------------------ source */

var prNumberPattern = regexp.MustCompile(`/pull/(\d+)`)

func liftSource(s *Source, note func(string, ...any)) *Source2 {
	if s == nil {
		return nil
	}
	out := &Source2{
		Kind: s.Kind, Provider: s.Provider, URL: s.URL, State: s.State,
		Revision: s.Commit, Label: s.Number,
	}
	if m := prNumberPattern.FindStringSubmatch(s.URL); m != nil {
		out.Identifier = m[1]
	}

	// cw/1 wrote a repository as owner/name and left the host to be assumed.
	// cw/2 wants the URL, and only one of those assumptions is safe to make.
	if s.Repo != "" {
		switch {
		case s.Provider == "github" || strings.Contains(s.URL, "github.com/"):
			out.RepositoryURL = "https://github.com/" + strings.Trim(s.Repo, "/")
		default:
			note("source.repo is %q and cw/2 wants a repositoryUrl. Nothing here says which host that is, so it was left out: write the full URL", s.Repo)
		}
	}

	if s.Base != "" || s.Head != "" {
		note("source.base and source.head are branch names, and cw/2's comparison holds two revisions, so they were dropped. gh pr view --json baseRefOid,headRefOid has the commits")
	}
	if len(s.ChangedFiles) > 0 {
		note("%d changedFiles were dropped: cw/2 records what happened to each file and cw/1 did not. gh pr view --json files, or git diff --name-status, says which. Without them a snippet links to the commit rather than into the diff", len(s.ChangedFiles))
	}
	return out
}

/* ------------------------------------------------------------------ blocks */

// liftStep keeps the order cw/1 rendered in, so a migrated walkthrough reads the
// way it read before: the prose, then the picture, then the change, then the
// code, then the thing to watch out for.
func liftStep(st *Step, ids *idSpace, at string, opt LiftOptions, note func(string, ...any), dropped *int) []Block {
	var out []Block
	if strings.TrimSpace(st.Body) != "" {
		out = append(out, Block{Type: BlockMarkdown, Text: st.Body})
	}
	if st.Diagram != nil {
		out = append(out, liftDiagram(st.Diagram, ids, at, note, dropped)...)
	}
	if st.Anim != nil {
		out = append(out, liftAnim(st.Anim, at, note))
	}
	if st.Diff != nil {
		out = append(out, liftDiff(st.Diff, at, opt, note))
	}
	if st.Code != nil {
		if st.Code.Check != nil {
			*dropped++
		}
		out = append(out, liftCode(st.Code, ""))
	}
	if strings.TrimSpace(st.Callout) != "" {
		out = append(out, Block{Type: BlockCallout, Severity: "warning", Text: st.Callout})
	}
	if len(out) == 0 {
		// A step has to hold at least one block, and a cw/1 step always had a
		// body, so this only happens for a body of nothing but whitespace.
		out = append(out, Block{Type: BlockMarkdown, Text: st.Title})
		note("%s had nothing but an empty body, so its title was used as the prose", at)
	}
	return out
}

func liftCode(c *Code, id string) Block {
	first := c.From
	if first == 0 {
		first = 1
	}
	rel := func(n int) LineRange { return LineRange{Start: n - first + 1} }

	s := &Snippet{Label: c.Note, Language: c.Lang, Text: c.Text}
	if c.From > 0 {
		// endLine and the hash are left to EnsureAnchors2, which counts and
		// hashes by cw/2's rules rather than carrying cw/1's answers over.
		s.Source = &Location{File: c.File, StartLine: c.From}
	}
	for _, r := range runs(c.Hi, first) {
		s.Highlights = append(s.Highlights, Highlight{Lines: r})
	}
	for _, r := range runs(c.Add, first) {
		s.Highlights = append(s.Highlights, Highlight{Lines: r, Kind: "added"})
	}
	for _, n := range c.Notes {
		s.Annotations = append(s.Annotations, Annotation{Lines: rel(n.Line), Text: n.Text})
	}
	return Block{Type: BlockCode, ID: id, Snippet: s}
}

// runs turns cw/1's list of single lines into cw/2's ranges, so six lines in a
// row read as one highlight rather than six.
func runs(lines []int, first int) []LineRange {
	if len(lines) == 0 {
		return nil
	}
	sorted := append([]int(nil), lines...)
	sort.Ints(sorted)
	var out []LineRange
	start, prev := sorted[0], sorted[0]
	for _, n := range sorted[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		out = append(out, span(start, prev, first))
		start, prev = n, n
	}
	return append(out, span(start, prev, first))
}

func span(from, to, first int) LineRange {
	r := LineRange{Start: from - first + 1}
	if to > from {
		r.End = to - first + 1
	}
	return r
}

func liftDiagram(dg *Diagram, ids *idSpace, at string, note func(string, ...any), dropped *int) []Block {
	// alt is required in cw/2 and has no equivalent in cw/1. A sentence saying
	// what a picture shows is not something to derive from the picture's source.
	note("%s.diagram needs an alt: one sentence saying what the picture shows, not what it looks like. It is the diagram for anybody who cannot see it", at)

	block := Block{Type: BlockDiagram, Format: "mermaid", Text: dg.Def, Caption: dg.Caption}
	if dg.Kind != "" && dg.Kind != "diagram" {
		// The label above the picture is gone; the picture says what it is.
		block.Caption = strings.TrimSpace(dg.Kind + ". " + dg.Caption)
	}

	keys := make([]string, 0, len(dg.Refs))
	for k := range dg.Refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	after := []Block{}
	for _, k := range keys {
		ref := dg.Refs[k]
		id := ids.claim("", k+"-code", at, note)
		block.Links = append(block.Links, DiagramLink{NodeID: k, BlockID: id})
		if ref.Check != nil {
			*dropped++
		}
		after = append(after, liftCode(&Code{
			File: ref.File, From: ref.From, Text: ref.Code, Note: ref.Label,
		}, id))
		if strings.TrimSpace(ref.Note) != "" {
			// A cw/2 snippet has a label but no note, so the sentence becomes a
			// paragraph under the code. The meaning survives, the layout does not.
			after = append(after, Block{Type: BlockMarkdown, Text: ref.Note})
			note("%s.diagram.refs[%q] had a note, which became a paragraph under the snippet because a cw/2 snippet has no note. Read it and decide whether it belongs there or in the label", at, k)
		}
	}
	return append([]Block{block}, after...)
}

var animStates = map[string]bool{"idle": true, "active": true, "done": true, "gone": true, "alert": true}

func liftAnim(a *Anim, at string, note func(string, ...any)) Block {
	b := Block{Type: BlockTimeline}
	if len(a.Frames) == 0 {
		return b
	}

	// cw/1 identified a node by its position in the frame, and refused frames of
	// different lengths, so the first frame is the node list.
	local := &idSpace{taken: map[string]bool{}}
	ids := make([]string, len(a.Frames[0].Nodes))
	for i, n := range a.Frames[0].Nodes {
		ids[i] = local.claim("", n.Label, at, func(string, ...any) {})
		b.Nodes = append(b.Nodes, TimelineNode{ID: ids[i], Label: n.Label})
	}

	for fi, f := range a.Frames {
		nf := TimelineFrame{Label: f.Label, Note: f.Note}
		for i, n := range f.Nodes {
			if i >= len(ids) {
				break
			}
			state := n.State
			if state == "" {
				// The page already read an empty state as idle, so this is the
				// rule written down rather than a guess.
				state = "idle"
			} else if !animStates[state] {
				note("%s.anim.frames[%d].nodes[%d] is in state %q, which is not one of idle, active, done, gone or alert. It was left as it is and the document will not validate until it is one of them", at, fi, i, state)
			}
			nf.States = append(nf.States, NodeState{NodeID: ids[i], State: state, Detail: n.Sub})
		}
		b.Frames = append(b.Frames, nf)
	}
	return b
}

/*
A cw/1 diff has one starting line and cw/2 needs one per side. They are only the
same number when the hunk is the first thing in the file, so taking it for both
is a guess that is usually wrong and always looks right.

So by default the lines are kept as a diff-flavoured snippet, which loses the
colouring and loses nothing else, and the note says where to get the real
coordinates.
*/
func liftDiff(df *Diff, at string, opt LiftOptions, note func(string, ...any)) Block {
	var ctx, add, del int
	var lines []HunkLine
	var text []string
	for _, l := range df.Lines {
		kind, mark := "context", " "
		switch l.Kind {
		case "add":
			kind, mark = "add", "+"
			add++
		case "del":
			kind, mark = "delete", "-"
			del++
		default:
			ctx++
		}
		lines = append(lines, HunkLine{Kind: kind, Text: l.T})
		text = append(text, mark+l.T)
	}

	if !opt.AssumeDiffStart || df.From <= 0 {
		why := "cw/1 gave a diff one starting line for both sides, and cw/2 needs one for each"
		if df.From <= 0 {
			why = "this diff had no starting line at all"
		}
		note("%s.diff was kept as a snippet rather than a diff: %s. git diff or gh pr diff on the revision has the real coordinates; --assume-diff-start takes the old number for both sides instead", at, why)
		return Block{
			Type: BlockCode,
			Snippet: &Snippet{
				Label: "was a diff of " + baseName(df.File), Language: "diff",
				Text: strings.Join(text, "\n"),
			},
		}
	}

	note("%s.diff was given %d as the starting line of both sides, because --assume-diff-start says so. Check it against the real diff", at, df.From)
	return Block{
		Type:   BlockDiff,
		Before: &Location{File: df.File},
		After:  &Location{File: df.File},
		Hunks: []DiffHunk{{
			OldStart: df.From, OldLines: ctx + del,
			NewStart: df.From, NewLines: ctx + add,
			Lines: lines,
		}},
	}
}

func baseName(p string) string {
	cut := strings.LastIndexAny(p, "/\\")
	if cut < 0 {
		return p
	}
	return p[cut+1:]
}
