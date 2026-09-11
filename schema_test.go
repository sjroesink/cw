package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// v2doc wraps one block in the smallest document that can legally hold it, so a
// test can say what it is about and nothing else.
func v2doc(block string) string {
	return `{"version":"cw/2","title":"T","parts":[{"id":"p","title":"P","sections":[
	  {"id":"s","title":"S","steps":[{"id":"st","title":"St","blocks":[` + block + `]}]}]}]}`
}

func check(t *testing.T, version, doc string) []string {
	t.Helper()
	sch := schemaFor(version)
	if sch == nil {
		t.Fatalf("no schema for %s", version)
	}
	var generic any
	if err := json.Unmarshal([]byte(doc), &generic); err != nil {
		t.Fatalf("the test document is not valid JSON: %v", err)
	}
	return sch.Validate(generic)
}

func wants(t *testing.T, got []string, substrings ...string) {
	t.Helper()
	joined := strings.Join(got, "\n")
	for _, want := range substrings {
		if !strings.Contains(joined, want) {
			t.Errorf("no complaint contains %q. Got:\n%s", want, joined)
		}
	}
}

func clean(t *testing.T, got []string) {
	t.Helper()
	if len(got) > 0 {
		t.Errorf("expected no complaints, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestBothVersionsHaveASchemaAndNothingElseDoes(t *testing.T) {
	for _, v := range []string{FormatV1, FormatV2} {
		if schemaFor(v) == nil {
			t.Errorf("%s has no schema, so a document saying so cannot be read", v)
		}
	}
	if schemaFor("cw/3") != nil {
		t.Error("a version this build does not know came back with a schema")
	}
}

// The schemas are only a contract while every rule in them is actually checked.
// A pattern that silently fails to compile is the worst kind of hole, because
// the validator keeps saying the document is fine.
func TestAPatternThatCannotBeCheckedStopsTheBuild(t *testing.T) {
	_, err := loadSchema([]byte(`{"type":"string","pattern":"^(?=x)y"}`))
	if err == nil {
		t.Fatal("a lookahead pattern loaded without complaint")
	}
	if !strings.Contains(err.Error(), "cannot check") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

func TestECMAPatternsAreRewrittenRatherThanDropped(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`^[a-z]+$(?![\s\S])`, `^[a-z]+$`},
		{`^[^\s\u0000-\u001f\u007f]+$`, `^[^\s\x{0000}-\x{001f}\x{007f}]+$`},
		{`^[^\\/:\u0000]+$`, `^[^\\/:\x{0000}]+$`},
	} {
		if got := re2Source(tc.in); got != tc.want {
			t.Errorf("re2Source(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The rewrite is only worth anything if the pattern then bites. A revision with
// a space in it is the case the pattern exists for.
func TestARewrittenPatternStillRefuses(t *testing.T) {
	doc := `{"version":"cw/2","title":"T","source":{"revision":"9f2 c1ab"},"parts":[
	  {"id":"p","title":"P","sections":[{"id":"s","title":"S","steps":[
	    {"id":"st","title":"St","blocks":[{"type":"markdown","text":"x"}]}]}]}]}`
	wants(t, check(t, FormatV2, doc), "source.revision", "does not match")
}

// portablePath is a stand-in for a regex RE2 cannot hold, so it has to be right
// on its own rather than by resemblance.
func TestThePortablePathStandIn(t *testing.T) {
	for _, ok := range []string{"a", "src/main.go", "a/b/c.ts", "a-b/c_d.e"} {
		if !portablePath(ok) {
			t.Errorf("%q should be a usable path", ok)
		}
	}
	for _, bad := range []string{"", "/abs", "a//b", "../up", "a/../b", "./here", "a/./b", `a\b`, "C:/x", "a/b/", "a\tb"} {
		if portablePath(bad) {
			t.Errorf("%q should not be a usable path", bad)
		}
	}
}

// A block is one of seven kinds, and a typo in the kind should read as one
// sentence naming the seven, not as seven sets of complaints about fields the
// author never meant to write.
func TestAnUnknownBlockKindIsOneSentence(t *testing.T) {
	got := check(t, FormatV2, v2doc(`{"type":"codeblock","text":"x"}`))
	if len(got) != 1 {
		t.Fatalf("expected one complaint, got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	wants(t, got, `"codeblock" is not one of`, "callout, code, diagram, diff, extension, markdown, reference, timeline")
}

// And once the kind is known, the complaints are that kind's own.
func TestABlockIsCheckedAgainstTheKindItSaysItIs(t *testing.T) {
	got := check(t, FormatV2, v2doc(`{"type":"code"}`))
	wants(t, got, `is missing "snippet"`)
	if strings.Contains(strings.Join(got, "\n"), "markdown") {
		t.Errorf("a code block was measured against the other kinds too:\n%s", strings.Join(got, "\n"))
	}
}

func TestABlockWithNoKindSaysWhichOnesThereAre(t *testing.T) {
	wants(t, check(t, FormatV2, v2doc(`{"text":"x"}`)),
		"is missing, and it is what says which kind this is", "markdown, reference, timeline")
}

// dependentRequired: endLine on its own is a line range with no beginning.
func TestAFieldThatOnlyMeansSomethingWithAnother(t *testing.T) {
	wants(t, check(t, FormatV2, v2doc(
		`{"type":"code","snippet":{"text":"x","source":{"file":"a.go","endLine":4}}}`)),
		"endLine", `only means something together with "startLine"`)
}

// propertyNames: ext is the one open object, and its keys are still namespaced.
func TestAnExtensionKeyHasToBeNamespaced(t *testing.T) {
	wants(t, check(t, FormatV2, v2doc(`{"type":"markdown","text":"x","ext":{"mine":1}}`)),
		`"mine" is not a name that can go here`, "Names here match")
	clean(t, check(t, FormatV2, v2doc(`{"type":"markdown","text":"x","ext":{"roesink/notes":1}}`)))
}

func TestAnEmptySourceIsWorseThanNoSource(t *testing.T) {
	doc := `{"version":"cw/2","title":"T","source":{},"parts":[{"id":"p","title":"P","sections":[
	  {"id":"s","title":"S","steps":[{"id":"st","title":"St","blocks":[{"type":"markdown","text":"x"}]}]}]}]}`
	wants(t, check(t, FormatV2, doc), "source: is empty, so leave it out")
}

func TestTheSameFileTwiceInAPart(t *testing.T) {
	doc := `{"version":"cw/2","title":"T","parts":[{"id":"p","title":"P","files":["a.go","a.go"],
	  "sections":[{"id":"s","title":"S","steps":[{"id":"st","title":"St","blocks":[
	    {"type":"markdown","text":"x"}]}]}]}]}`
	wants(t, check(t, FormatV2, doc), "parts[0].files[1]", "says the same as [0]")
}

// if/then: a rename is only a rename when it says what it was called before.
func TestARenameHasToSayWhatItWasCalled(t *testing.T) {
	base := `{"version":"cw/2","title":"T","source":{"changedFiles":[{"file":"b.go","status":%q}]},
	  "parts":[{"id":"p","title":"P","sections":[{"id":"s","title":"S","steps":[
	    {"id":"st","title":"St","blocks":[{"type":"markdown","text":"x"}]}]}]}]}`
	wants(t, check(t, FormatV2, strings.Replace(base, "%q", `"renamed"`, 1)),
		`is missing "previousFile"`)
	clean(t, check(t, FormatV2, strings.Replace(base, "%q", `"modified"`, 1)))
}

// if/then/else with a not in the else: dirty is a fact about a working tree, so
// it is required there and meaningless against a revision.
func TestDirtyBelongsToAWorkingTreeAndNowhereElse(t *testing.T) {
	code := func(against string) string {
		return v2doc(`{"type":"code","snippet":{"text":"x","source":{"file":"a.go"},
		  "verification":{"state":"match","checkedAt":"2026-09-09T10:00:00Z","against":` + against + `}}}`)
	}
	wants(t, check(t, FormatV2, code(`{"kind":"working-tree","revision":"9f2c1ab"}`)),
		`is missing "dirty"`)
	wants(t, check(t, FormatV2, code(`{"kind":"revision","revision":"9f2c1ab","dirty":false}`)),
		`must not have "dirty" here`)
	clean(t, check(t, FormatV2, code(`{"kind":"revision","revision":"9f2c1ab"}`)))
	clean(t, check(t, FormatV2, code(`{"kind":"working-tree","revision":"9f2c1ab","dirty":true}`)))
}

// anyOf over two required lists reads as a sentence about the two fields.
func TestADiffNeedsAtLeastOneSide(t *testing.T) {
	wants(t, check(t, FormatV2, v2doc(`{"type":"diff","hunks":[]}`)),
		`needs at least one of "after", "before"`)
}

// A new file has nothing on the old side, and saying so is the whole point of
// leaving before out. A delete line there is a contradiction.
func TestANewFileHasNothingToDelete(t *testing.T) {
	wants(t, check(t, FormatV2, v2doc(`{"type":"diff","after":{"file":"a.go"},"hunks":[
	  {"oldStart":0,"oldLines":0,"newStart":1,"newLines":1,"lines":[{"kind":"delete","text":"x"}]}]}`)),
		"has to be add, not delete")
}

func TestAValidWalkthroughSaysNothing(t *testing.T) {
	clean(t, check(t, FormatV2, v2doc(`{"type":"code","id":"c","snippet":{
	  "label":"Simplified","language":"typescript","text":"const a = 1;\nconst b = 2;\n",
	  "source":{"file":"src/a.ts","startLine":10,"endLine":11},
	  "highlights":[{"lines":{"start":1}}],
	  "annotations":[{"lines":{"start":1,"end":2},"text":"Both of them."}]}}`)))
}

// cw/1 has to keep validating exactly as it did, because eighteen of them are
// published and none of them are going to be rewritten.
func TestTheOldSchemaStillRefusesTheOldMistakes(t *testing.T) {
	doc := `{"version":"cw/1","title":"T","parts":[{"title":"P","sections":[
	  {"title":"S","steps":[{"title":"St","body":"b","nonsense":1}]}]}]}`
	wants(t, check(t, FormatV1, doc), "is not a field here")
	clean(t, check(t, FormatV1, `{"version":"cw/1","title":"T","parts":[{"title":"P","sections":[
	  {"title":"S","steps":[{"title":"St","body":"b"}]}]}]}`))
}
