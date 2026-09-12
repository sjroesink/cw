package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func read2(t *testing.T, doc string) *LoadResult {
	t.Helper()
	res, err := ParseDoc([]byte(doc), "test.json")
	if err != nil {
		t.Fatalf("the fixture does not even load: %v", err)
	}
	return res
}

func wantErr(t *testing.T, doc string, substrings ...string) {
	t.Helper()
	got := strings.Join(read2(t, doc).Errors, "\n")
	for _, want := range substrings {
		if !strings.Contains(got, want) {
			t.Errorf("no error contains %q. Got:\n%s", want, got)
		}
	}
	if got == "" {
		t.Error("no errors at all")
	}
}

func wantNoErr(t *testing.T, doc string) {
	t.Helper()
	if errs := read2(t, doc).Errors; len(errs) > 0 {
		t.Errorf("expected no errors, got:\n%s", strings.Join(errs, "\n"))
	}
}

// The version in the file decides everything after it, so it is worth one test
// on its own that both of them arrive as the right kind of document.
func TestTheVersionPicksTheDocument(t *testing.T) {
	one := read2(t, `{"version":"cw/1","title":"T","parts":[{"title":"P","sections":[
	  {"title":"S","steps":[{"title":"St","body":"b"}]}]}]}`)
	if one.Doc == nil || one.Doc2 != nil || one.View().Format != FormatV1 {
		t.Errorf("a cw/1 document came back as %+v", one.View())
	}
	two := read2(t, v2doc(`{"type":"markdown","text":"x"}`))
	if two.Doc2 == nil || two.Doc != nil || two.View().Format != FormatV2 {
		t.Errorf("a cw/2 document came back as %+v", two.View())
	}
	if _, err := ParseDoc([]byte(`{"version":"cw/9","title":"T","parts":[]}`), "x.json"); err == nil {
		t.Error("a version this build does not know was read anyway")
	}
}

// A document upgraded by hand keeps pointing an editor at the old contract, and
// then the editor says the file is wrong. Nothing breaks, so it is a warning.
func TestASchemaURLThatDisagreesWithTheVersion(t *testing.T) {
	doc := `{"$schema":"https://cw.roesink.dev/schema/v1.json","version":"cw/2","title":"T","parts":[
	  {"id":"p","title":"P","sections":[{"id":"s","title":"S","steps":[
	    {"id":"st","title":"St","blocks":[{"type":"markdown","text":"x"}]}]}]}]}`
	got := strings.Join(read2(t, doc).Warnings, "\n")
	if !strings.Contains(got, "$schema points at cw/1 while version says cw/2") {
		t.Errorf("no warning about the mismatch:\n%s", got)
	}
}

// ---------------------------------------------------------------- counting

/*
This is the one place cw/2 deliberately disagrees with cw/1. cw/1 trimmed every
trailing newline before hashing, so a file that ends with one and a file that
does not came out the same. cw/2 keeps it, and counts lines the other way round.
*/
func TestASnippetIsCountedAndHashedByDifferentRules(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"one", 1},
		{"one\n", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
		{"one\ntwo\n\n", 3},
		{"one\r\ntwo\r\n", 2},
	} {
		if got := snippetLines2(tc.text); got != tc.want {
			t.Errorf("snippetLines2(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}

	if snippetHash2("a\n") == snippetHash2("a") {
		t.Error("a trailing newline hashes the same as none, which is what cw/1 did and cw/2 does not")
	}
	if snippetHash2("a\r\nb") != snippetHash2("a\nb") {
		t.Error("the line ending a file happens to be checked out with changed the hash")
	}
	// And cw/1 is left exactly as it was, because eighteen published documents
	// have hashes written under its rule.
	if snippetSha("a\n") != snippetSha("a") {
		t.Error("the cw/1 hash changed, which invalidates every document already published")
	}
}

func TestTheHashHasToMatchTheText(t *testing.T) {
	bad := `{"type":"code","snippet":{"text":"a\nb","hash":{"algorithm":"sha256",
	  "value":"0000000000000000000000000000000000000000000000000000000000000000"}}}`
	wantErr(t, v2doc(bad), "the hash does not match the text")
}

// A source range is a claim that the snippet is a verbatim excerpt, so the two
// numbers and the text have to be able to be true at the same time.
func TestASourceRangeHasToBeAsLongAsTheText(t *testing.T) {
	wantErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb","source":
	  {"file":"a.go","startLine":10,"endLine":14}}}`),
		"lines 10 to 14 is 5 lines and the text has 2")
	wantNoErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb","source":
	  {"file":"a.go","startLine":10,"endLine":11}}}`))
	// A snippet with no end has not been through publish yet, and that is fine.
	wantNoErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb","source":
	  {"file":"a.go","startLine":10}}}`))
}

// Snippet-relative is the whole point, so pointing at the line in the file is
// the mistake worth naming out loud.
func TestHighlightsAreCountedFromTheSnippet(t *testing.T) {
	wantErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb","source":
	  {"file":"a.go","startLine":40,"endLine":41},"highlights":[{"lines":{"start":41}}]}}`),
		"the snippet is 2 lines long", "counted from 1, not from the line in the file")
	wantErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb",
	  "annotations":[{"lines":{"start":2,"end":1},"text":"x"}]}}`),
		"lines end at 1 and start at 2")
	wantNoErr(t, v2doc(`{"type":"code","snippet":{"text":"a\nb",
	  "highlights":[{"lines":{"start":1,"end":2},"kind":"added"}]}}`))
}

func TestAVerificationTimestampHasToBeOne(t *testing.T) {
	wantErr(t, v2doc(`{"type":"code","snippet":{"text":"a","source":{"file":"a.go"},
	  "verification":{"state":"match","checkedAt":"last Tuesday",
	    "against":{"kind":"revision","revision":"9f2c1ab"}}}}`),
		"is not a timestamp like")
}

// ---------------------------------------------------------------- identity

// cw/1 had a namespace per level. cw/2 has one for the document, because a
// diagram link names a block and nothing in the name says what kind of thing it
// is pointing at.
func TestIdsAreUniqueAcrossTheWholeDocument(t *testing.T) {
	doc := `{"version":"cw/2","title":"T","parts":[
	  {"id":"thing","title":"P","sections":[{"id":"s","title":"S","steps":[
	    {"id":"thing","title":"St","blocks":[{"type":"markdown","text":"x"}]}]}]}]}`
	wantErr(t, doc, `the id "thing" is already used by parts[0] (P)`, "one namespace for the whole document")
}

// The overview and the part pages hold blocks of their own. A block there is a
// block: the same schema, the same checks on what is inside it, and the same one
// namespace for the whole document.
func TestABlockOnAPageIsHeldToWhatABlockIsHeldTo(t *testing.T) {
	step := `"sections":[{"id":"s","title":"S","steps":[{"id":"st","title":"St","blocks":[` +
		`{"type":"markdown","id":"words","text":"x"}]}]}]`
	overview := `{"version":"cw/2","title":"T","blocks":[%s],"parts":[{"id":"p","title":"P",` + step + `}]}`
	part := `{"version":"cw/2","title":"T","parts":[{"id":"p","title":"P","blocks":[%s],` + step + `}]}`

	// What is inside it is checked, and the path says which page it is on.
	badSnippet := `{"type":"code","snippet":{"text":"a\nb","source":{"file":"a.go","startLine":10,"endLine":14}}}`
	wantErr(t, fmt.Sprintf(overview, badSnippet),
		"the walkthrough.blocks[0]", "lines 10 to 14 is 5 lines and the text has 2")
	wantErr(t, fmt.Sprintf(part, badSnippet), "parts[0] (P).blocks[0]")

	// And its id is claimed out of the one namespace the document has.
	wantErr(t, fmt.Sprintf(overview, `{"type":"markdown","id":"words","text":"y"}`),
		`the id "words" is already used`, "one namespace for the whole document")
	wantNoErr(t, fmt.Sprintf(overview, `{"type":"markdown","id":"other","text":"y"}`))
	wantNoErr(t, fmt.Sprintf(part, `{"type":"callout","severity":"tip","text":"y"}`))
}

// A snippet on one of those pages is a snippet: it is checked against the
// working tree, it is in the file list, and cw check has a line to print it on.
// The overview is not part one, so it is a page with no number.
func TestASnippetOnAPageIsCheckedLikeAnyOther(t *testing.T) {
	view := read2(t, `{"version":"cw/2","title":"T",
	  "blocks":[{"type":"code","snippet":{"text":"a\n","source":{"file":"over.go","startLine":1}}}],
	  "parts":[{"id":"p","title":"P",
	    "blocks":[{"type":"code","snippet":{"text":"b\n","source":{"file":"part.go","startLine":1}}}],
	    "sections":[{"id":"s","title":"S","steps":[{"id":"st","title":"St","blocks":[
	      {"type":"code","snippet":{"text":"c\n","source":{"file":"step.go","startLine":1}}}]}]}]}]}`).View()

	if len(view.Snippets) != 3 {
		t.Errorf("%d snippets are checked against the tree, want 3", len(view.Snippets))
	}
	if want := []string{"over.go", "part.go", "step.go"}; !reflect.DeepEqual(view.Files, want) {
		t.Errorf("the file list is %v, want %v", view.Files, want)
	}
	if view.Parts != 1 {
		t.Errorf("the walkthrough says it has %d parts, and the overview is not one of them", view.Parts)
	}

	tour := view.Tour()
	if len(tour) != 2 {
		t.Fatalf("cw check would print %d pages, want the overview and one part", len(tour))
	}
	if tour[0].Number != 0 || tour[0].Title != "the overview" || len(tour[0].Snippets) != 1 {
		t.Errorf("the overview came back as %+v", tour[0])
	}
	if tour[1].Number != 1 || len(tour[1].Snippets) != 1 || len(tour[1].Sections) != 1 {
		t.Errorf("the part came back as %+v", tour[1])
	}
}

func TestADiagramCanOnlyLinkToCodeThatExists(t *testing.T) {
	diagram := func(target string) string {
		return v2doc(`{"type":"markdown","id":"words","text":"x"},
		  {"type":"code","id":"real","snippet":{"text":"a"}},
		  {"type":"diagram","format":"mermaid","alt":"A picture.",
		   "text":"flowchart LR\n  node[n]","links":[{"nodeId":"node","blockId":"` + target + `"}]}`)
	}
	wantErr(t, diagram("missing"), `there is no block with the id "missing"`)
	wantErr(t, diagram("words"), `"words" is a markdown block, and a diagram can only link to code`)
	wantNoErr(t, diagram("real"))
}

// A link to a shape the picture does not have is not an error, because the
// mermaid may be right and the id wrong or the other way round. It is a warning
// because nothing will be clickable and nobody will know why.
func TestALinkToAShapeThatIsNotInThePicture(t *testing.T) {
	res := read2(t, v2doc(`{"type":"code","id":"real","snippet":{"text":"a"}},
	  {"type":"diagram","format":"mermaid","alt":"A picture.","text":"flowchart LR\n  here[n]",
	   "links":[{"nodeId":"elsewhere","blockId":"real"}]}`))
	if len(res.Errors) > 0 {
		t.Errorf("this should not refuse the document: %v", res.Errors)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "never mentions") {
		t.Errorf("no warning about the missing shape: %v", res.Warnings)
	}
}

// ---------------------------------------------------------------- diffs

// A hunk says how big it is twice, once in its counts and once in its lines.
// The counts are the half nobody looks at, so they are the half that rots.
func TestAHunkCountsItsOwnLines(t *testing.T) {
	hunk := func(old, new int) string {
		return v2doc(`{"type":"diff","after":{"file":"a.go"},"before":{"file":"a.go"},"hunks":[
		  {"oldStart":1,"oldLines":` + itoa(old) + `,"newStart":1,"newLines":` + itoa(new) + `,"lines":[
		    {"kind":"context","text":"keep"},
		    {"kind":"delete","text":"was"},
		    {"kind":"add","text":"is"}]}]}`)
	}
	wantErr(t, hunk(9, 2), "oldLines is 9 and the hunk has 1 context and 1 deleted lines, which is 2")
	wantErr(t, hunk(2, 9), "newLines is 9 and the hunk has 1 context and 1 added lines, which is 2")
	wantNoErr(t, hunk(2, 2))
}

func TestHunksWalkDownAFileWithoutOverlapping(t *testing.T) {
	two := func(secondStart int) string {
		one := `{"oldStart":10,"oldLines":2,"newStart":10,"newLines":2,"lines":[
		  {"kind":"context","text":"a"},{"kind":"context","text":"b"}]}`
		second := `{"oldStart":` + itoa(secondStart) + `,"oldLines":1,"newStart":40,"newLines":1,"lines":[
		  {"kind":"context","text":"c"}]}`
		return v2doc(`{"type":"diff","before":{"file":"a.go"},"after":{"file":"a.go"},
		  "hunks":[` + one + `,` + second + `]}`)
	}
	wantErr(t, two(11), "the old side starts at 11 and the hunk before it ran to 11, so they overlap")
	wantNoErr(t, two(12))
}

// A missing newline at the end of a file is a fact about the last line of a
// side, so it cannot be said about a line in the middle.
func TestNoNewlineAtEndBelongsAtTheEnd(t *testing.T) {
	diff := func(onFirst bool) string {
		first, second := "false", "true"
		if onFirst {
			first, second = "true", "false"
		}
		return v2doc(`{"type":"diff","before":{"file":"a.go"},"after":{"file":"a.go"},"hunks":[
		  {"oldStart":1,"oldLines":2,"newStart":1,"newLines":2,"lines":[
		    {"kind":"context","text":"a","noNewlineAtEnd":` + first + `},
		    {"kind":"context","text":"b","noNewlineAtEnd":` + second + `}]}]}`)
	}
	wantErr(t, diff(true), "not the last line on its side")
	wantNoErr(t, diff(false))
}

// ---------------------------------------------------------------- timelines

// Every frame is a whole picture. That is what lets a reader jump to the third
// frame without having drawn the first two.
func TestEveryFrameHasEveryNodeExactlyOnce(t *testing.T) {
	frame := func(states string) string {
		return v2doc(`{"type":"timeline","nodes":[{"id":"a","label":"A"},{"id":"b","label":"B"}],
		  "frames":[{"label":"One","states":[` + states + `]}]}`)
	}
	wantErr(t, frame(`{"nodeId":"a","state":"active"}`),
		`is missing a state for "b"`, "a node that is not doing anything is idle rather than absent")
	wantErr(t, frame(`{"nodeId":"a","state":"active"},{"nodeId":"b","state":"idle"},{"nodeId":"b","state":"done"}`),
		`"b" already has a state in this frame`)
	wantErr(t, frame(`{"nodeId":"a","state":"active"},{"nodeId":"b","state":"idle"},{"nodeId":"c","state":"idle"}`),
		`"c" is not one of this timeline's nodes`)
	wantNoErr(t, frame(`{"nodeId":"a","state":"active"},{"nodeId":"b","state":"idle"}`))

	wantErr(t, v2doc(`{"type":"timeline","nodes":[{"id":"a","label":"A"},{"id":"a","label":"Again"}],
	  "frames":[{"label":"One","states":[{"nodeId":"a","state":"idle"}]}]}`),
		`"a" is defined twice in this timeline`)
}

// ---------------------------------------------------------------- round trip

// The struct has to cover the whole schema. Anything it does not know about is
// dropped on the way through publish, and a field that quietly disappears is the
// kind of bug that only shows up in somebody else's reader.
func TestTheTourSurvivesBeingReadAndWrittenAgain(t *testing.T) {
	raw, err := os.ReadFile("examples/cw2-tour.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseDoc(raw, "cw2-tour.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("the example does not validate:\n%s", strings.Join(res.Errors, "\n"))
	}

	again, err := res.View().Raw()
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		was, now := firstDifference(raw, again)
		t.Errorf("the document changed on its way through the struct.\nwas:  %s\nnow:  %s", was, now)
	}
}

// An empty hunks list is a real thing to say: a file that was renamed and not
// otherwise touched. It is also the one field that omitempty would eat.
func TestAnEmptyHunkListSurvives(t *testing.T) {
	in := `{"type":"diff","before":{"file":"old.go"},"after":{"file":"new.go"},"hunks":[]}`
	res := read2(t, v2doc(in))
	out, err := res.View().Raw()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"hunks":[]`) {
		t.Errorf("the empty hunk list did not come back:\n%s", out)
	}
}

// firstDifference is only for the failure message, so it is allowed to be
// rough: the first line the two disagree on says enough to find the field.
func firstDifference(a, b []byte) (string, string) {
	as, bs := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	for i := range as {
		if i >= len(bs) {
			return as[i], "(nothing)"
		}
		if as[i] != bs[i] {
			return as[i], bs[i]
		}
	}
	return "(the same)", "(the same)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}
