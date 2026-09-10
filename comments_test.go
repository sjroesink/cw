package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testReader is the local server the way the browser meets it: a walkthrough on
// disk, a token, and a comment store pointed somewhere temporary.
func testReader(t *testing.T) (*server, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "wt.json")
	if err := os.WriteFile(file, []byte(goodDoc), 0o644); err != nil {
		t.Fatalf("cannot write the walkthrough: %v", err)
	}
	commentsDir = func() (string, error) { return filepath.Join(dir, "state", "comments"), nil }
	servingStore = func() (string, error) { return filepath.Join(dir, "state", "servers.json"), nil }

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatalf("no web assets in this build: %v", err)
	}
	title := "A change worth reading"
	return &server{file: file, root: dir, token: "a-token", vendor: NewVendor(true), web: sub,
		key: "wt", title: title, comments: openComments("wt", title)}, file
}

func ask(t *testing.T, s *server, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("X-Cw-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const oneComment = `{"where":{"step":"the-fix/how/it-reads-the-file","title":"It reads the file",
  "block":"code","kind":"code","file":"a.go","lines":{"start":3,"end":5},"quote":"func main() {}"},
  "text":"why does this stop here?"}`

// The walkthrough is the author's file, and a reader is not an author. Asking a
// question about it must leave it byte for byte where it was, or a comment
// becomes something that has to be undone before the file can be published.
func TestACommentIsNeverWrittenIntoTheWalkthrough(t *testing.T) {
	s, file := testReader(t)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("cannot read the walkthrough back: %v", err)
	}

	rec, out := ask(t, s, "POST", "/api/comments", oneComment)
	if rec.Code != http.StatusOK {
		t.Fatalf("leaving a comment returned %d: %s", rec.Code, rec.Body)
	}
	if out["thread"] == nil {
		t.Fatal("nothing came back, so the page has nothing to show")
	}

	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("cannot read the walkthrough back: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the walkthrough changed when somebody commented on it")
	}

	_, out = ask(t, s, "GET", "/api/comments", "")
	threads, _ := out["threads"].([]any)
	if len(threads) != 1 {
		t.Fatalf("the page is shown %d threads, want 1", len(threads))
	}
	if out["prompt"] == nil || !strings.Contains(out["prompt"].(string), "cw comments watch wt") {
		t.Errorf("the prompt to copy does not name the walkthrough: %v", out["prompt"])
	}
}

// Two agents watching the same walkthrough is a thing that happens by accident,
// and both answering the same question is the part a reader would notice.
func TestOnlyOneWatcherGetsAComment(t *testing.T) {
	s, _ := testReader(t)
	ask(t, s, "POST", "/api/comments", oneComment)

	_, first := ask(t, s, "POST", "/api/comments/next", "{}")
	if first["thread"] == nil {
		t.Fatal("the first watcher was given nothing to answer")
	}
	_, second := ask(t, s, "POST", "/api/comments/next", "{}")
	if second["thread"] != nil {
		t.Error("a second watcher was handed the same comment")
	}

	_, out := ask(t, s, "GET", "/api/comments", "")
	threads, _ := out["threads"].([]any)
	one, _ := threads[0].(map[string]any)
	if one["status"] != "thinking" {
		t.Errorf("the page says the comment is %v, so it shows no spinner", one["status"])
	}
	if out["watching"] != true {
		t.Error("the page says nobody is watching while a watcher is asking for work")
	}
}

// An agent that fell over halfway must not take the question with it.
func TestAClaimThatWentNowhereIsHandedOutAgain(t *testing.T) {
	s, _ := testReader(t)
	ask(t, s, "POST", "/api/comments", oneComment)
	ask(t, s, "POST", "/api/comments/next", "{}")

	long := time.Now().Add(-claimPatience - time.Minute)
	s.comments.mu.Lock()
	s.comments.file.Threads[0].PickedUp = &long
	s.comments.mu.Unlock()

	_, again := ask(t, s, "POST", "/api/comments/next", "{}")
	if again["thread"] == nil {
		t.Error("a comment nobody came back for is waiting forever")
	}
}

// An answer closes a thread; asking again under it opens it, because a follow-up
// question that nobody is handed is a question nobody answers.
func TestAnAnswerClosesTheThreadAndAskingAgainOpensIt(t *testing.T) {
	s, _ := testReader(t)
	_, out := ask(t, s, "POST", "/api/comments", oneComment)
	id := out["thread"].(map[string]any)["id"].(string)

	ask(t, s, "POST", "/api/comments/"+id, `{"from":"agent","text":"Because the file ends there."}`)
	if _, none := ask(t, s, "POST", "/api/comments/next", "{}"); none["thread"] != nil {
		t.Error("an answered comment is still being handed out")
	}

	ask(t, s, "POST", "/api/comments/"+id, `{"from":"reader","text":"and the line above it?"}`)
	_, again := ask(t, s, "POST", "/api/comments/next", "{}")
	if again["thread"] == nil {
		t.Fatal("asking again left the thread closed")
	}
	msgs := again["thread"].(map[string]any)["messages"].([]any)
	if len(msgs) != 3 {
		t.Errorf("the agent is handed %d messages, so it cannot see what it already said", len(msgs))
	}
}

// The threads outlive the run. Somebody opens the same walkthrough again
// tomorrow and their questions, and the answers, are still beside the code.
func TestCommentsSurviveTheServerStopping(t *testing.T) {
	s, _ := testReader(t)
	ask(t, s, "POST", "/api/comments", oneComment)

	again := openComments("wt", "A change worth reading")
	view := again.view("")
	if len(view.Threads) != 1 {
		t.Fatalf("%d threads came back from disk, want 1", len(view.Threads))
	}
	if view.Threads[0].Messages[0].Text != "why does this stop here?" {
		t.Errorf("what came back is not what was asked: %q", view.Threads[0].Messages[0].Text)
	}
	if view.Threads[0].Where.File != "a.go" || view.Threads[0].Where.Lines.End != 5 {
		t.Error("where the comment hangs did not survive the write")
	}
}

// An answer may be written in the blocks a step is written in, and then it is
// drawn by the renderer that draws the step. What that renderer can draw is
// what the schema allows, so this is the line between the two.
func TestAnAnswerInBlocksIsHeldToTheSchema(t *testing.T) {
	s, _ := testReader(t)
	_, out := ask(t, s, "POST", "/api/comments", oneComment)
	id := out["thread"].(map[string]any)["id"].(string)

	bad := `{"from":"agent","blocks":[{"type":"code","snippet":{"text":"x"},"highlights":[]}]}`
	rec, wrong := ask(t, s, "POST", "/api/comments/"+id, bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a block with a field in the wrong place was accepted (%d)", rec.Code)
	}
	if errs, _ := wrong["errors"].([]any); len(errs) == 0 {
		t.Error("it refused without saying which field, which is nothing an agent can act on")
	}

	good := `{"from":"agent","blocks":[
	  {"type":"markdown","text":"Because the file ends there."},
	  {"type":"code","snippet":{"language":"go","text":"func main() {}\n",
	    "source":{"file":"a.go","startLine":3,"endLine":3}}}]}`
	rec, out = ask(t, s, "POST", "/api/comments/"+id, good)
	if rec.Code != http.StatusOK {
		t.Fatalf("a good answer in blocks was refused (%d): %s", rec.Code, rec.Body)
	}
	msgs := out["thread"].(map[string]any)["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	blocks, _ := last["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("%d blocks came back, want 2", len(blocks))
	}
	if last["text"] != nil {
		t.Error("the answer carries prose as well as blocks, so the page has two things to draw")
	}

	// And it survives the round trip through the file, which is where a struct
	// that drops a field it does not know about would show up.
	again := openComments("wt", "A change worth reading")
	kept := again.view("").Threads[0].Messages
	if got := kept[len(kept)-1].Blocks; len(got) != 2 || got[1].Snippet == nil ||
		got[1].Snippet.Source.File != "a.go" {
		t.Errorf("what came back from disk is not what was answered: %+v", got)
	}
}

// Archiving is putting away, and nothing here throws a comment out. What
// somebody asked is a record of a reading, and the answer under it is often the
// most useful thing on the step.
func TestAnArchivedCommentIsPutAwayRatherThanLost(t *testing.T) {
	s, _ := testReader(t)
	_, out := ask(t, s, "POST", "/api/comments", oneComment)
	id := out["thread"].(map[string]any)["id"].(string)

	rec, _ := ask(t, s, "POST", "/api/comments/"+id+"/archive", `{"on":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("archiving returned %d: %s", rec.Code, rec.Body)
	}
	_, list := ask(t, s, "GET", "/api/comments", "")
	threads, _ := list["threads"].([]any)
	if len(threads) != 1 {
		t.Fatalf("%d threads left, want the one that was put away", len(threads))
	}
	if threads[0].(map[string]any)["archived"] != true {
		t.Error("it came back without saying it was archived, so the page cannot hide it")
	}

	// And a watcher is not handed something the reader has put away.
	if _, none := ask(t, s, "POST", "/api/comments/next", "{}"); none["thread"] != nil {
		t.Error("an archived comment is still being handed out to answer")
	}

	ask(t, s, "POST", "/api/comments/"+id+"/archive", `{"on":false}`)
	_, back := ask(t, s, "POST", "/api/comments/next", "{}")
	if back["thread"] == nil {
		t.Error("taking it back out of the archive left it unanswerable")
	}
}

// The site has no working tree, no editor and nobody in a terminal to answer
// with, and a write path for anonymous readers is not something gate.go should
// have to guard. So it has none of this.
func TestTheHostedSiteHasNoComments(t *testing.T) {
	h, _ := testHost(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/comments"},
		{"POST", "/api/comments"},
		{"POST", "/api/comments/next"},
		{"POST", "/api/comments/1"},
		{"DELETE", "/api/comments/1"},
	} {
		rec, _ := do(t, h, c.method, c.path, "", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s returned %d on the site, want 404", c.method, c.path, rec.Code)
		}
	}
}

// The name a walkthrough's comments are filed under is the one thing that has
// to be the same tomorrow, and the same from the other terminal.
func TestTwoWalkthroughsWithOneNameDoNotShareComments(t *testing.T) {
	a := commentKey("", filepath.Join("C:", "one", "walkthrough.json"))
	b := commentKey("", filepath.Join("C:", "two", "walkthrough.json"))
	if a == b {
		t.Errorf("both are filed as %q", a)
	}
	if !strings.HasPrefix(a, "walkthrough-") {
		t.Errorf("the file is called %q, which says nothing about which walkthrough it is", a)
	}
	if got := commentKey("fincent-pr-3579", "/tmp/x.json"); got != "fincent-pr-3579" {
		t.Errorf("a published walkthrough is filed as %q rather than under its name", got)
	}
}
