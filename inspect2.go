package main

import (
	"fmt"
	"strings"
	"time"
)

/*
The schema says what shape a cw/2 document has. This says the things a schema
cannot: that a number in one place agrees with a count somewhere else, that a
name points at something that exists, and that a picture is complete.

Errors refuse the document. Warnings are about whether it is worth reading, and
they never refuse: a walkthrough with too many parts is a walkthrough.
*/

func inspect2(res *LoadResult, d *Doc2) {
	errf := func(f string, a ...any) { res.Errors = append(res.Errors, fmt.Sprintf(f, a...)) }
	warnf := func(f string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(f, a...)) }

	if strings.TrimSpace(d.Summary) == "" {
		warnf("summary is empty, so the overview opens on a row of cards and no argument")
	}
	if len(d.Parts) > 6 {
		warnf("%d parts. The overview is a set of cards to choose from, not a list to scroll", len(d.Parts))
	}

	// cw/1 gave each level its own namespace. cw/2 has one for the document,
	// because a diagram link names a block and nothing in the name says what kind
	// of thing it is pointing at.
	taken := map[string]string{}
	claim := func(id, at string) {
		if id == "" {
			return
		}
		if first, dup := taken[id]; dup {
			errf("%s: the id %q is already used by %s, and cw/2 has one namespace for the whole document", at, id, first)
			return
		}
		taken[id] = at
	}

	blocks := map[string]*Block{}
	steps, bare := 0, 0

	for pi := range d.Parts {
		p := &d.Parts[pi]
		pat := fmt.Sprintf("parts[%d] (%s)", pi, p.Title)
		claim(p.ID, pat)
		if len(p.Sections) > 5 {
			warnf("%s holds %d sections. Over about four, the part is two parts", pat, len(p.Sections))
		}
		if strings.TrimSpace(p.Summary) == "" {
			warnf("%s has no summary, so its card is a title on its own", pat)
		}
		for si := range p.Sections {
			s := &p.Sections[si]
			sat := fmt.Sprintf("parts[%d].sections[%d] (%s)", pi, si, s.Title)
			claim(s.ID, sat)
			if len(s.Steps) > 6 {
				warnf("%s holds %d steps. Over about five, a section stops being one idea", sat, len(s.Steps))
			}
			for ii := range s.Steps {
				st := &s.Steps[ii]
				iat := fmt.Sprintf("parts[%d].sections[%d].steps[%d] (%s)", pi, si, ii, st.Title)
				claim(st.ID, iat)
				steps++
				if onlyProse(st) {
					bare++
					warnf("%s is prose only: no code, no diagram, no diff, no timeline", iat)
				}
				for bi := range st.Blocks {
					b := &st.Blocks[bi]
					bat := fmt.Sprintf("%s.blocks[%d] (%s)", iat, bi, b.Type)
					claim(b.ID, bat)
					if b.ID != "" {
						blocks[b.ID] = b
					}
					checkBlock(errf, warnf, b, bat)
				}
			}
		}
	}

	// Links are checked after the walk because a diagram may point at a block
	// that comes later, and often does: the picture first, then the code.
	d.walkBlocks(func(at string, b *Block) {
		if b.Type != BlockDiagram {
			return
		}
		for li, link := range b.Links {
			target, known := blocks[link.BlockID]
			switch {
			case !known:
				errf("%s.links[%d]: there is no block with the id %q", at, li, link.BlockID)
			case target.Type != BlockCode:
				errf("%s.links[%d]: %q is a %s block, and a diagram can only link to code",
					at, li, link.BlockID, target.Type)
			}
			if !strings.Contains(b.Text, link.NodeID) {
				warnf("%s.links[%d]: the mermaid source never mentions %q, so no shape will be clickable for it",
					at, li, link.NodeID)
			}
		}
	})

	if bare > 0 && bare*3 > steps {
		warnf("%d of %d steps are prose only. This is a code walkthrough: if the code is not the point, write a document instead", bare, steps)
	}
}

// onlyProse is the step that could have been a paragraph in a document.
func onlyProse(st *Step2) bool {
	for _, b := range st.Blocks {
		switch b.Type {
		case BlockCode, BlockDiagram, BlockDiff, BlockTimeline, BlockExtension:
			return false
		}
	}
	return true
}

func checkBlock(errf, warnf func(string, ...any), b *Block, at string) {
	switch b.Type {
	case BlockCode:
		checkSnippet(errf, warnf, b.Snippet, at+".snippet")
	case BlockDiff:
		checkHunks(errf, warnf, b.Hunks, at)
	case BlockTimeline:
		checkTimeline(errf, b, at)
	}
}

func checkSnippet(errf, warnf func(string, ...any), s *Snippet, at string) {
	if s == nil {
		return
	}
	lines := snippetLines2(s.Text)

	if s.Hash != nil && s.Hash.Value != snippetHash2(s.Text) {
		errf("%s: the hash does not match the text, so one of them was edited after the other. Leave it out and it is filled in on publish", at)
	}

	// A source range is a claim that the snippet is a verbatim excerpt, so the
	// two numbers and the text have to be able to be true at the same time.
	if src := s.Source; src != nil {
		switch {
		case src.EndLine > 0 && src.StartLine == 0:
			errf("%s.source: endLine without startLine is a range with no beginning", at)
		case src.EndLine > 0 && src.EndLine < src.StartLine:
			errf("%s.source: endLine %d is before startLine %d", at, src.EndLine, src.StartLine)
		case src.EndLine > 0 && src.EndLine-src.StartLine+1 != lines:
			errf("%s.source: lines %d to %d is %d lines and the text has %d. Leave endLine out and it is filled in on publish",
				at, src.StartLine, src.EndLine, src.EndLine-src.StartLine+1, lines)
		}
	}

	inside := func(r LineRange, what string, i int) {
		if r.End > 0 && r.End < r.Start {
			errf("%s.%s[%d]: lines end at %d and start at %d", at, what, i, r.End, r.Start)
			return
		}
		if r.Start < 1 || r.last() > lines {
			errf("%s.%s[%d] points at lines %d to %d, and the snippet is %d lines long. Snippet lines are counted from 1, not from the line in the file",
				at, what, i, r.Start, r.last(), lines)
		}
	}
	for i, h := range s.Highlights {
		inside(h.Lines, "highlights", i)
	}
	for i, a := range s.Annotations {
		inside(a.Lines, "annotations", i)
	}
	if n := len(s.Annotations); n > 4 {
		warnf("%s has %d annotations. Past a handful they stop being asides and become the text", at, n)
	}

	if v := s.Verification; v != nil {
		if _, err := time.Parse(time.RFC3339, v.CheckedAt); err != nil {
			errf("%s.verification.checkedAt: %q is not a timestamp like 2026-09-09T10:00:00Z", at, v.CheckedAt)
		}
	}
}

/*
A hunk says twice how big it is: once in its counts and once in its lines. When
those disagree the diff cannot be applied and cannot be drawn either, and the
counts are the half a reader never sees, so they are the half that rots.

The other rule is order. Hunks walk down a file, so each one starts after the
last one ended, on both sides at once.
*/
func checkHunks(errf, warnf func(string, ...any), hunks []DiffHunk, at string) {
	prevOld, prevNew := 0, 0
	changed := false

	for hi, h := range hunks {
		hat := fmt.Sprintf("%s.hunks[%d]", at, hi)
		var ctx, add, del int
		for _, l := range h.Lines {
			switch l.Kind {
			case "context":
				ctx++
			case "add":
				add++
			case "delete":
				del++
			}
		}
		if add > 0 || del > 0 {
			changed = true
		}
		if want := ctx + del; h.OldLines != want {
			errf("%s: oldLines is %d and the hunk has %d context and %d deleted lines, which is %d",
				hat, h.OldLines, ctx, del, want)
		}
		if want := ctx + add; h.NewLines != want {
			errf("%s: newLines is %d and the hunk has %d context and %d added lines, which is %d",
				hat, h.NewLines, ctx, add, want)
		}

		prevOld = afterHunk(errf, hat, "old", h.OldStart, h.OldLines, prevOld)
		prevNew = afterHunk(errf, hat, "new", h.NewStart, h.NewLines, prevNew)
	}

	checkEndMarkers(errf, hunks, at)

	if len(hunks) > 0 && !changed {
		warnf("%s has no added or deleted lines, so it is a snippet with extra ceremony", at)
	}
}

// afterHunk checks one side of one hunk against where the previous one ended,
// and answers where this one ends. An empty range is a position between two
// lines rather than a stretch of them, so it may sit exactly where the last
// hunk stopped.
func afterHunk(errf func(string, ...any), at, side string, start, count, prevEnd int) int {
	if count == 0 {
		if start < prevEnd {
			errf("%s: the %s side starts at %d and the hunk before it ran to %d", at, side, start, prevEnd)
		}
		return max(start, prevEnd)
	}
	if start <= prevEnd {
		errf("%s: the %s side starts at %d and the hunk before it ran to %d, so they overlap", at, side, start, prevEnd)
	}
	return start + count - 1
}

// A missing newline at the end of a file is a fact about the last line of a
// side, so it can only be said about the last line of that side.
func checkEndMarkers(errf func(string, ...any), hunks []DiffHunk, at string) {
	type spot struct{ h, l int }
	var lastOld, lastNew spot
	seen := false
	for hi, h := range hunks {
		for li, l := range h.Lines {
			seen = true
			if l.Kind == "context" || l.Kind == "delete" {
				lastOld = spot{hi, li}
			}
			if l.Kind == "context" || l.Kind == "add" {
				lastNew = spot{hi, li}
			}
		}
	}
	if !seen {
		return
	}
	for hi, h := range hunks {
		for li, l := range h.Lines {
			if !l.NoNewlineAtEnd {
				continue
			}
			here := spot{hi, li}
			ok := (l.Kind != "add" && here == lastOld) || (l.Kind != "delete" && here == lastNew)
			if !ok {
				errf("%s.hunks[%d].lines[%d]: noNewlineAtEnd is about the end of a file, and this is not the last line on its side",
					at, hi, li)
			}
		}
	}
}

// Every frame is a whole picture. That is what lets a reader jump to the third
// frame without having drawn the first two, and it is why a frame that forgets a
// node is a broken frame rather than an implied one.
func checkTimeline(errf func(string, ...any), b *Block, at string) {
	defined := map[string]bool{}
	for i, n := range b.Nodes {
		if defined[n.ID] {
			errf("%s.nodes[%d]: %q is defined twice in this timeline", at, i, n.ID)
			continue
		}
		defined[n.ID] = true
	}

	for fi, f := range b.Frames {
		fat := fmt.Sprintf("%s.frames[%d] (%s)", at, fi, f.Label)
		here := map[string]bool{}
		for si, s := range f.States {
			switch {
			case !defined[s.NodeID]:
				errf("%s.states[%d]: %q is not one of this timeline's nodes", fat, si, s.NodeID)
			case here[s.NodeID]:
				errf("%s.states[%d]: %q already has a state in this frame", fat, si, s.NodeID)
			}
			here[s.NodeID] = true
		}
		var missing []string
		for _, n := range b.Nodes {
			if !here[n.ID] {
				missing = append(missing, n.ID)
			}
		}
		if len(missing) > 0 {
			errf("%s is missing a state for %s. Every frame is a complete picture, so a node that is not doing anything is idle rather than absent",
				fat, strings.Join(quoteAll(missing), ", "))
		}
	}
}

// ---------------------------------------------------------------- walking

// walkBlocks visits every block in reading order, with the path an author can
// find it by.
func (d *Doc2) walkBlocks(fn func(at string, b *Block)) {
	for pi := range d.Parts {
		p := &d.Parts[pi]
		for si := range p.Sections {
			s := &p.Sections[si]
			for ii := range s.Steps {
				st := &s.Steps[ii]
				for bi := range st.Blocks {
					fn(fmt.Sprintf("parts[%d].sections[%d].steps[%d].blocks[%d] (%s)",
						pi, si, ii, bi, st.Blocks[bi].Type), &st.Blocks[bi])
				}
			}
		}
	}
}
