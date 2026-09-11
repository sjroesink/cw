package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

/*
cw/2 is the same three levels as cw/1, and a different step.

Where cw/1 gave a step fixed slots, at most one of each and in an order the page
decided, cw/2 gives it a list of blocks in the order the author wrote them. Two
snippets with a paragraph between them is a sentence the format can say now.

Everything else follows from that. Ids are required and unique across the whole
document, because a block is addressable and blocks are what diagrams link to.
Line numbers inside a snippet are relative to the snippet, so moving the excerpt
up a file changes one number instead of all of them. A diff carries real
unified-diff coordinates for both sides. A timeline frame is a complete snapshot
rather than a picture that has to be lined up with the previous one by eye. And
`extension` is the one door out, carrying the fallback that makes a reader who
does not know it still readable.
*/

// The standard block kinds. Custom content uses extension.
const (
	BlockMarkdown  = "markdown"
	BlockCode      = "code"
	BlockCallout   = "callout"
	BlockDiagram   = "diagram"
	BlockDiff      = "diff"
	BlockTimeline  = "timeline"
	BlockExtension = "extension"
	BlockReference = "reference"
)

type Doc2 struct {
	Schema   string                     `json:"$schema,omitempty"`
	Version  string                     `json:"version"`
	Title    string                     `json:"title"`
	Summary  string                     `json:"summary,omitempty"`
	Language string                     `json:"language,omitempty"`
	Source   *Source2                   `json:"source,omitempty"`
	Parts    []Part2                    `json:"parts"`
	Ext      map[string]json.RawMessage `json:"ext,omitempty"`
}

// Source2 is where the walkthrough came from. It says it in URLs and revisions
// rather than in the owner/name shorthand cw/1 used, so that a walkthrough about
// something that is not on GitHub is still saying something true.
type Source2 struct {
	Kind          string                     `json:"kind,omitempty"`
	Provider      string                     `json:"provider,omitempty"`
	RepositoryURL string                     `json:"repositoryUrl,omitempty"`
	URL           string                     `json:"url,omitempty"`
	Identifier    string                     `json:"identifier,omitempty"`
	Label         string                     `json:"label,omitempty"`
	State         string                     `json:"state,omitempty"`
	Revision      string                     `json:"revision,omitempty"`
	Comparison    *Comparison                `json:"comparison,omitempty"`
	ChangedFiles  []FileChange               `json:"changedFiles,omitempty"`
	Ext           map[string]json.RawMessage `json:"ext,omitempty"`
}

type Comparison struct {
	BaseRevision string `json:"baseRevision"`
	HeadRevision string `json:"headRevision"`
}

type FileChange struct {
	File         string `json:"file"`
	PreviousFile string `json:"previousFile,omitempty"`
	Status       string `json:"status"`
}

type Part2 struct {
	ID          string                     `json:"id"`
	Title       string                     `json:"title"`
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	Files       []string                   `json:"files,omitempty"`
	Sections    []Section2                 `json:"sections"`
	Ext         map[string]json.RawMessage `json:"ext,omitempty"`
}

type Section2 struct {
	ID      string                     `json:"id"`
	Title   string                     `json:"title"`
	Summary string                     `json:"summary,omitempty"`
	Steps   []Step2                    `json:"steps"`
	Ext     map[string]json.RawMessage `json:"ext,omitempty"`
}

type Step2 struct {
	ID     string                     `json:"id"`
	Title  string                     `json:"title"`
	Speech string                     `json:"speech,omitempty"`
	Blocks []Block                    `json:"blocks"`
	Ext    map[string]json.RawMessage `json:"ext,omitempty"`
}

// Block is every kind of block in one struct, told apart by Type. The schema
// closes each kind to its own fields, so a value that reaches here has already
// been refused if it mixed two of them, and one flat struct beats seven types
// that every caller would have to switch over twice.
//
// What it costs is the marshalling, which cannot be the default: a diff's hunks
// may legitimately be empty, and there is no struct tag that means "always write
// this field, but only when this is a diff". MarshalJSON below writes the fields
// of the kind and nothing else, which is also what keeps a round trip honest.
type Block struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	// markdown and callout
	Text string `json:"text,omitempty"`

	// callout
	Severity string `json:"severity,omitempty"`
	Title    string `json:"title,omitempty"`

	// code
	Snippet *Snippet `json:"snippet,omitempty"`

	// diagram
	Format string        `json:"format,omitempty"`
	Alt    string        `json:"alt,omitempty"`
	Links  []DiagramLink `json:"links,omitempty"`

	// diagram, diff and timeline
	Caption string `json:"caption,omitempty"`

	// diff
	Before   *Location  `json:"before,omitempty"`
	After    *Location  `json:"after,omitempty"`
	Language string     `json:"language,omitempty"`
	Hunks    []DiffHunk `json:"hunks,omitempty"`

	// timeline
	Nodes  []TimelineNode  `json:"nodes,omitempty"`
	Frames []TimelineFrame `json:"frames,omitempty"`

	// extension
	Name     string          `json:"name,omitempty"`
	Version  int             `json:"version,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Fallback string          `json:"fallback,omitempty"`

	// reference
	Description string           `json:"description,omitempty"`
	Relation    string           `json:"relation,omitempty"`
	Target      *ReferenceTarget `json:"target,omitempty"`

	Ext map[string]json.RawMessage `json:"ext,omitempty"`
}

type ReferenceTarget struct {
	File   string `json:"file,omitempty"`
	URL    string `json:"url,omitempty"`
	StepID string `json:"stepId,omitempty"`
}

type Snippet struct {
	Label        string        `json:"label,omitempty"`
	Language     string        `json:"language,omitempty"`
	Text         string        `json:"text"`
	Source       *Location     `json:"source,omitempty"`
	Hash         *ContentHash  `json:"hash,omitempty"`
	Highlights   []Highlight   `json:"highlights,omitempty"`
	Annotations  []Annotation  `json:"annotations,omitempty"`
	Verification *Verification `json:"verification,omitempty"`
}

// Location is a place in a repository. Its repositoryUrl and revision fall back
// to the document's source, but only while they agree about which repository
// this is: naming a different one turns the inheritance off, so a walkthrough
// that reaches into a second repository cannot quietly pick the first one's
// commit. See spec/FORMAT.md.
type Location struct {
	File          string `json:"file"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	Revision      string `json:"revision,omitempty"`
	StartLine     int    `json:"startLine,omitempty"`
	EndLine       int    `json:"endLine,omitempty"`
	URL           string `json:"url,omitempty"`
}

type ContentHash struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// LineRange is inclusive, and always counted from the first line of the snippet
// rather than from the first line of the file.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end,omitempty"`
}

func (r LineRange) last() int {
	if r.End > 0 {
		return r.End
	}
	return r.Start
}

type Highlight struct {
	Lines LineRange `json:"lines"`
	Kind  string    `json:"kind,omitempty"`
}

type Annotation struct {
	Lines LineRange `json:"lines"`
	Text  string    `json:"text"`
}

// Verification is a check somebody made at a moment that has passed. It is
// provenance, not news: a reader holding the repository works the answer out for
// itself and ignores this.
type Verification struct {
	State          string    `json:"state"`
	CheckedAt      string    `json:"checkedAt"`
	Against        Against   `json:"against"`
	ResolvedSource *Location `json:"resolvedSource,omitempty"`
	Note           string    `json:"note,omitempty"`
}

// Against carries Dirty as a pointer because false is a real answer about a
// working tree and no answer at all is the only right one about a revision.
type Against struct {
	Kind     string `json:"kind"`
	Revision string `json:"revision"`
	Dirty    *bool  `json:"dirty,omitempty"`
}

type DiagramLink struct {
	NodeID  string `json:"nodeId"`
	BlockID string `json:"blockId"`
}

type HunkLine struct {
	Kind           string `json:"kind"`
	Text           string `json:"text"`
	NoNewlineAtEnd bool   `json:"noNewlineAtEnd,omitempty"`
}

type DiffHunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Heading  string     `json:"heading,omitempty"`
	Lines    []HunkLine `json:"lines"`
}

type TimelineNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type NodeState struct {
	NodeID string `json:"nodeId"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// TimelineFrame is a complete picture. Every node the timeline defines appears
// in it exactly once, which is what lets a reader draw a frame without having
// seen the one before it.
type TimelineFrame struct {
	Label      string      `json:"label"`
	Note       string      `json:"note,omitempty"`
	DurationMs int         `json:"durationMs,omitempty"`
	States     []NodeState `json:"states"`
}

// ---------------------------------------------------------------- marshalling

type blockHead struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type extTail struct {
	Ext map[string]json.RawMessage `json:"ext,omitempty"`
}

func (b Block) MarshalJSON() ([]byte, error) {
	head := blockHead{Type: b.Type, ID: b.ID}
	tail := extTail{Ext: b.Ext}
	switch b.Type {
	case BlockMarkdown:
		return marshalPlain(struct {
			blockHead
			Text string `json:"text"`
			extTail
		}{head, b.Text, tail})
	case BlockCode:
		return marshalPlain(struct {
			blockHead
			Snippet *Snippet `json:"snippet"`
			extTail
		}{head, b.Snippet, tail})
	case BlockCallout:
		return marshalPlain(struct {
			blockHead
			Severity string `json:"severity,omitempty"`
			Title    string `json:"title,omitempty"`
			Text     string `json:"text"`
			extTail
		}{head, b.Severity, b.Title, b.Text, tail})
	case BlockDiagram:
		return marshalPlain(struct {
			blockHead
			Format  string        `json:"format"`
			Text    string        `json:"text"`
			Alt     string        `json:"alt"`
			Caption string        `json:"caption,omitempty"`
			Links   []DiagramLink `json:"links,omitempty"`
			extTail
		}{head, b.Format, b.Text, b.Alt, b.Caption, b.Links, tail})
	case BlockDiff:
		hunks := b.Hunks
		if hunks == nil {
			hunks = []DiffHunk{}
		}
		return marshalPlain(struct {
			blockHead
			Before   *Location  `json:"before,omitempty"`
			After    *Location  `json:"after,omitempty"`
			Language string     `json:"language,omitempty"`
			Caption  string     `json:"caption,omitempty"`
			Hunks    []DiffHunk `json:"hunks"`
			extTail
		}{head, b.Before, b.After, b.Language, b.Caption, hunks, tail})
	case BlockTimeline:
		return marshalPlain(struct {
			blockHead
			Caption string          `json:"caption,omitempty"`
			Nodes   []TimelineNode  `json:"nodes"`
			Frames  []TimelineFrame `json:"frames"`
			extTail
		}{head, b.Caption, b.Nodes, b.Frames, tail})
	case BlockReference:
		return marshalPlain(struct {
			blockHead
			Title       string           `json:"title"`
			Description string           `json:"description,omitempty"`
			Relation    string           `json:"relation,omitempty"`
			Target      *ReferenceTarget `json:"target"`
			extTail
		}{head, b.Title, b.Description, b.Relation, b.Target, tail})
	case BlockExtension:
		return marshalPlain(struct {
			blockHead
			Name     string          `json:"name"`
			Version  int             `json:"version"`
			Data     json.RawMessage `json:"data"`
			Fallback string          `json:"fallback"`
			extTail
		}{head, b.Name, b.Version, b.Data, b.Fallback, tail})
	}
	// An unknown kind is a document that never passed validation. Writing it out
	// as it came in is better than losing it on the way to the error.
	type plain Block
	return marshalPlain(plain(b))
}

// ---------------------------------------------------------------- snippet text

/*
cw/1 folded CRLF to LF and then trimmed every trailing newline before hashing,
which made a file that ends with a newline and one that does not hash the same.
cw/2 keeps the newline, because the difference is real and a diff will show it.

Counting is the other way round: a snippet of three lines ending in a newline is
three lines, not four. So the two rules are stated separately here rather than
sharing one normalisation, which is what made them the same rule in cw/1.
*/

func snippetText2(text string) string { return strings.ReplaceAll(text, "\r\n", "\n") }

func snippetLines2(text string) int {
	s := snippetText2(text)
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n") + 1
	if strings.HasSuffix(s, "\n") {
		n--
	}
	return n
}

func snippetHash2(text string) string {
	sum := sha256.Sum256([]byte(snippetText2(text)))
	return hex.EncodeToString(sum[:])
}

// marshalPlain is json.Marshal without the HTML escaping. A walkthrough is prose
// about code and full of <, > and &, and turning those into \u003c makes a file
// somebody has to edit unreadable for the sake of embedding it in a script tag,
// which is not something this format ever does.
func marshalPlain(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
