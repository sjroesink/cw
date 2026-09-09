package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A walkthrough: one pull request or one subsystem, split into parts, sections
// and steps. Nothing in the page or the server knows the topic, so a new
// walkthrough is this file and nothing else.
//
// The format is cw/1 and it is written to outlive this reader: an IDE plugin or
// something that plays it out loud should be able to take the same file. That is
// why the location of a snippet is stated in full rather than implied, and why
// parts, sections and steps carry an id that survives an edit.
type Doc struct {
	Schema   string         `json:"$schema,omitempty"`
	Version  string         `json:"version"`
	Title    string         `json:"title"`
	Source   *Source        `json:"source,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Root     string         `json:"root,omitempty"`
	Language string         `json:"language,omitempty"`
	Parts    []Part         `json:"parts"`
	Ext      map[string]any `json:"ext,omitempty"`
}

// FormatVersion is the only version this build reads and writes.
const FormatVersion = "cw/1"

// Source is where the walkthrough came from, in a form something other than a
// human can act on. It is what turns a path plus a line number into a link.
type Source struct {
	Kind         string   `json:"kind,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Repo         string   `json:"repo,omitempty"`
	Number       string   `json:"number,omitempty"`
	URL          string   `json:"url,omitempty"`
	State        string   `json:"state,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Base         string   `json:"base,omitempty"`
	Head         string   `json:"head,omitempty"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
}

type Part struct {
	ID       string         `json:"id,omitempty"`
	Title    string         `json:"title"`
	Desc     string         `json:"desc,omitempty"`
	Long     string         `json:"long,omitempty"`
	Files    []string       `json:"files,omitempty"`
	Sections []Section      `json:"sections"`
	Ext      map[string]any `json:"ext,omitempty"`
}

type Section struct {
	ID    string         `json:"id,omitempty"`
	Title string         `json:"title"`
	Desc  string         `json:"desc,omitempty"`
	Steps []Step         `json:"steps"`
	Ext   map[string]any `json:"ext,omitempty"`
}

type Step struct {
	ID      string         `json:"id,omitempty"`
	Title   string         `json:"title"`
	Body    string         `json:"body"`
	Speech  string         `json:"speech,omitempty"`
	Callout string         `json:"callout,omitempty"`
	Diagram *Diagram       `json:"diagram,omitempty"`
	Code    *Code          `json:"code,omitempty"`
	Diff    *Diff          `json:"diff,omitempty"`
	Anim    *Anim          `json:"anim,omitempty"`
	Ext     map[string]any `json:"ext,omitempty"`
}

type Diagram struct {
	Kind    string          `json:"kind,omitempty"`
	Def     string          `json:"def"`
	Caption string          `json:"caption,omitempty"`
	Refs    map[string]*Ref `json:"refs,omitempty"`
}

type Ref struct {
	Label string `json:"label,omitempty"`
	File  string `json:"file"`
	From  int    `json:"from,omitempty"`
	To    int    `json:"to,omitempty"`
	Sha   string `json:"sha,omitempty"`
	Note  string `json:"note,omitempty"`
	Code  string `json:"code"`
	Check *Check `json:"check,omitempty"`
}

type Code struct {
	File  string     `json:"file"`
	From  int        `json:"from,omitempty"`
	To    int        `json:"to,omitempty"`
	Sha   string     `json:"sha,omitempty"`
	Lang  string     `json:"lang,omitempty"`
	Note  string     `json:"note,omitempty"`
	Text  string     `json:"text"`
	Hi    []int      `json:"hi,omitempty"`
	Add   []int      `json:"add,omitempty"`
	Notes []LineNote `json:"notes,omitempty"`
	Check *Check     `json:"check,omitempty"`
}

type LineNote struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type Diff struct {
	File  string     `json:"file"`
	From  int        `json:"from,omitempty"`
	Lines []DiffLine `json:"lines"`
}

type DiffLine struct {
	Kind string `json:"kind"`
	T    string `json:"t"`
}

type Anim struct {
	Frames []Frame `json:"frames"`
}

type Frame struct {
	Label string      `json:"label"`
	Note  string      `json:"note,omitempty"`
	Nodes []FrameNode `json:"nodes"`
}

type FrameNode struct {
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
	State string `json:"state,omitempty"`
}

// Check is what the working tree says about a pasted snippet. The page shows it
// next to the file name, because a snippet that no longer exists is the one
// thing a walkthrough cannot notice about itself.
type Check struct {
	State string `json:"state"` // ok, moved, gone, missing-file, outside-root, unchecked
	Line  int    `json:"line,omitempty"`
	Note  string `json:"note,omitempty"`
}

// ---------------------------------------------------------------- loading

type LoadResult struct {
	Doc      *Doc
	Errors   []string
	Warnings []string
}

func LoadDoc(path string, sch *schemaDoc) (*LoadResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDoc(raw, path, sch)
}

// ParseDoc is LoadDoc without the file, so the API validates the bytes a
// publisher posts through exactly the path cw check walks. The name is only
// used to say where a complaint came from.
func ParseDoc(raw []byte, name string, sch *schemaDoc) (*LoadResult, error) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", name, err)
	}

	res := &LoadResult{}
	// A file written before cw/1 is lifted first, so the validator sees the
	// shape it knows and the author gets errors about their walkthrough rather
	// than about a format they never chose.
	if obj, ok := generic.(map[string]any); ok && migrateLegacy(obj) {
		res.Warnings = append(res.Warnings,
			"this file was written before cw/1 and was read as if it had been migrated. Run cw migrate to make that permanent")
	}
	res.Errors = sch.Validate(generic)

	lifted, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("%s could not be re-read after migration: %w", name, err)
	}
	d := &Doc{}
	if err := json.Unmarshal(lifted, d); err != nil {
		return nil, fmt.Errorf("%s does not match the schema: %w", name, err)
	}
	res.Doc = d
	inspect(res, d)
	return res, nil
}

// inspect covers what a schema cannot say: that a highlighted line is inside the
// snippet it highlights, that a diagram ref points at a block the diagram has,
// that a section is not secretly two.
func inspect(res *LoadResult, d *Doc) {
	errf := func(f string, a ...any) { res.Errors = append(res.Errors, fmt.Sprintf(f, a...)) }
	warnf := func(f string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(f, a...)) }

	if strings.TrimSpace(d.Summary) == "" {
		warnf("summary is empty, so the overview opens on a row of cards and no argument")
	}
	if len(d.Parts) > 6 {
		warnf("%d parts. The overview is a set of cards to choose from, not a list to scroll", len(d.Parts))
	}

	// An id is what a reader's progress and every deep link hang off, so two of
	// them in one place is worse than none: it silently sends people to the
	// wrong step.
	partIDs := map[string]bool{}
	unique := func(seen map[string]bool, id, at string) {
		if id == "" {
			return
		}
		if seen[id] {
			errf("%s: the id %q is already taken at this level, so a link to it is ambiguous", at, id)
		}
		seen[id] = true
	}

	steps, bare := 0, 0
	for pi := range d.Parts {
		p := &d.Parts[pi]
		at := fmt.Sprintf("parts[%d] (%s)", pi, p.Title)
		if len(p.Sections) > 5 {
			warnf("%s holds %d sections. Over about four, the part is two parts", at, len(p.Sections))
		}
		if strings.TrimSpace(p.Desc) == "" {
			warnf("%s has no desc, so its card is a title on its own", at)
		}
		unique(partIDs, p.ID, at)
		sectionIDs := map[string]bool{}
		for si := range p.Sections {
			s := &p.Sections[si]
			sat := fmt.Sprintf("parts[%d].sections[%d] (%s)", pi, si, s.Title)
			if len(s.Steps) > 6 {
				warnf("%s holds %d steps. Over about five, a section stops being one idea", sat, len(s.Steps))
			}
			unique(sectionIDs, s.ID, sat)
			stepIDs := map[string]bool{}
			for ii := range s.Steps {
				st := &s.Steps[ii]
				iat := fmt.Sprintf("parts[%d].sections[%d].steps[%d] (%s)", pi, si, ii, st.Title)
				steps++
				unique(stepIDs, st.ID, iat)
				if st.Diagram == nil && st.Code == nil && st.Diff == nil && st.Anim == nil {
					bare++
					warnf("%s shows nothing: no diagram, no code, no diff, no animation", iat)
				}
				if n := len([]rune(st.Body)); n > 700 {
					warnf("%s: body is %d characters. The column reads best under about 450", iat, n)
				}
				checkStep(errf, warnf, st, iat)
			}
		}
	}

	if bare > 0 && bare*3 > steps {
		warnf("%d of %d steps are prose only. This is a code walkthrough: if the code is not the point, write a document instead", bare, steps)
	}
}

func checkStep(errf, warnf func(string, ...any), st *Step, at string) {
	if c := st.Code; c != nil {
		lines := strings.Split(strings.ReplaceAll(c.Text, "\r\n", "\n"), "\n")
		first, last := c.first(), c.first()+len(lines)-1
		inRange := func(kind string, n int, i int) {
			if n < first || n > last {
				errf("%s.code.%s[%d] points at line %d, and the snippet runs %d to %d", at, kind, i, n, first, last)
			}
		}
		for i, n := range c.Hi {
			inRange("hi", n, i)
		}
		for i, n := range c.Add {
			inRange("add", n, i)
		}
		seen := map[int]bool{}
		for i, note := range c.Notes {
			inRange("notes", note.Line, i)
			if seen[note.Line] {
				errf("%s.code.notes[%d]: line %d already has a note, and only one of them can open", at, i, note.Line)
			}
			seen[note.Line] = true
		}
		if len(c.Notes) > 4 {
			warnf("%s.code has %d line notes. Past a handful they stop being asides and become the text", at, len(c.Notes))
		}
		anchorErrs(errf, at+".code", c.From, c.To, c.Sha, c.Text)
	}

	if dg := st.Diagram; dg != nil {
		for key, ref := range dg.Refs {
			if !strings.Contains(dg.Def, key) {
				warnf("%s.diagram.refs[%q]: the mermaid source never mentions %q, so no block will be clickable for it", at, key, key)
			}
			if ref.Label == "" {
				ref.Label = key
			}
			anchorErrs(errf, fmt.Sprintf("%s.diagram.refs[%q]", at, key), ref.From, ref.To, ref.Sha, ref.Code)
		}
	}

	if df := st.Diff; df != nil {
		changed := false
		for _, l := range df.Lines {
			if l.Kind != "ctx" {
				changed = true
			}
		}
		if !changed {
			warnf("%s.diff has no add or del lines, so it is a snippet with extra ceremony", at)
		}
	}

	if an := st.Anim; an != nil && len(an.Frames) > 0 {
		want := len(an.Frames[0].Nodes)
		for i, f := range an.Frames {
			if len(f.Nodes) != want {
				errf("%s.anim.frames[%d] has %d nodes and the first frame has %d. The frames are the same nodes changing state, not different pictures", at, i, len(f.Nodes), want)
			}
		}
	}
}

// anchorErrs holds to and sha to what the text actually is. Both are written by
// cw publish rather than by hand, so a mismatch means the snippet was edited
// afterwards and the anchor is now a lie a consumer would act on.
func anchorErrs(errf func(string, ...any), at string, from, to int, sha, text string) {
	if to != 0 {
		want := from + snippetLines(text) - 1
		if from == 0 {
			want = snippetLines(text)
		}
		if to != want {
			errf("%s: to is %d and the snippet ends at %d. Leave it out and it is filled in on publish", at, to, want)
		}
	}
	if sha != "" && sha != snippetSha(text) {
		errf("%s: sha does not match the text, so it was edited after the hash was written. Leave it out and it is filled in on publish", at)
	}
}

func (c *Code) first() int {
	if c.From > 0 {
		return c.From
	}
	return 1
}

// ---------------------------------------------------------------- the tree

// Tree turns file paths in the walkthrough into real files, and refuses
// anything that climbs out of the root. It is the only place an untrusted path
// becomes an absolute one.
type Tree struct {
	Root string
}

func (t *Tree) safePath(rel string) (string, string, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if clean == "" {
		return "", "", fmt.Errorf("empty path")
	}
	abs := filepath.Clean(filepath.Join(t.Root, filepath.FromSlash(clean)))
	rp, err := filepath.Rel(t.Root, abs)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s lies outside the root", rel)
	}
	return abs, filepath.ToSlash(rp), nil
}

// Verify asks the working tree whether the pasted code is still there. It is a
// weaker promise than resolving the code live, and it is the one this format can
// keep: the page says what it found rather than showing yesterday's code as if
// it were today's.
func (t *Tree) Verify(d *Doc) (checked, moved, stale int) {
	tally := func(c *Check) {
		checked++
		switch c.State {
		case "ok", "unchecked":
		case "moved":
			moved++
		default:
			stale++
		}
	}
	for pi := range d.Parts {
		for si := range d.Parts[pi].Sections {
			for ii := range d.Parts[pi].Sections[si].Steps {
				st := &d.Parts[pi].Sections[si].Steps[ii]
				if st.Code != nil {
					st.Code.Check = t.find(st.Code.File, st.Code.Text, st.Code.From)
					tally(st.Code.Check)
				}
				if st.Diagram == nil {
					continue
				}
				for _, ref := range st.Diagram.Refs {
					ref.Check = t.find(ref.File, ref.Code, ref.From)
					tally(ref.Check)
				}
			}
		}
	}
	return checked, moved, stale
}

// find looks for the block in the file: first where the walkthrough says it is,
// then anywhere. Moving code is not the same as changing it, and the reader is
// told which of the two happened.
func (t *Tree) find(rel, block string, from int) *Check {
	if t.Root == "" {
		return &Check{State: "unchecked"}
	}
	abs, path, err := t.safePath(rel)
	if err != nil {
		return &Check{State: "outside-root", Note: err.Error()}
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return &Check{State: "missing-file", Note: path + " is not in the working tree"}
	}

	have := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	want := strings.Split(strings.TrimRight(strings.ReplaceAll(block, "\r\n", "\n"), "\n"), "\n")
	if len(want) == 0 || len(want) > len(have) {
		return &Check{State: "gone"}
	}

	matches := func(start int) bool {
		for i, line := range want {
			if strings.TrimRight(have[start+i], " \t") != strings.TrimRight(line, " \t") {
				return false
			}
		}
		return true
	}

	if from > 0 && from-1+len(want) <= len(have) && matches(from-1) {
		return &Check{State: "ok", Line: from}
	}
	for start := 0; start+len(want) <= len(have); start++ {
		if !matches(start) {
			continue
		}
		if from > 0 && start+1 != from {
			return &Check{State: "moved", Line: start + 1,
				Note: fmt.Sprintf("the same lines now start at %d, not %d", start+1, from)}
		}
		return &Check{State: "ok", Line: start + 1}
	}
	return &Check{State: "gone", Note: "these lines are no longer in " + path}
}

// Steps counts what the header progress bar divides by.
func (d *Doc) Steps() int {
	n := 0
	for _, p := range d.Parts {
		for _, s := range p.Sections {
			n += len(s.Steps)
		}
	}
	return n
}

// Files lists every path the walkthrough points at, once, in reading order.
func (d *Doc) Files() []string {
	var out []string
	seen := map[string]bool{}
	add := func(f string) {
		if f == "" || seen[f] {
			return
		}
		seen[f] = true
		out = append(out, f)
	}
	for _, p := range d.Parts {
		for _, s := range p.Sections {
			for _, st := range s.Steps {
				if st.Code != nil {
					add(st.Code.File)
				}
				if st.Diff != nil {
					add(st.Diff.File)
				}
				if st.Diagram != nil {
					for _, r := range st.Diagram.Refs {
						add(r.File)
					}
				}
			}
		}
	}
	return out
}
