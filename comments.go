package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* A comment is the one thing on this page a reader writes rather than reads.
   They select a few lines or half a sentence, say what they want to know, and
   an agent sitting in the terminal next to them answers it.

   It is local only, and it is not part of the walkthrough. Which checkout the
   paths hang off is a fact about the machine reading the document rather than
   about the document, and so is a question somebody had while reading it: the
   file on disk is never touched, and the site has none of these routes.

   The server is the only writer. The command talks to it over loopback rather
   than editing the same file from a second process, which is what makes "who
   gets this comment" a decision taken in one place. */

// ---------------------------------------------------------------- what one is

const (
	commentsFormat = "cw-comments/1"

	// A watcher says it is there by asking for work. Ten seconds is a good few
	// of those polls, so one slow answer is not the page announcing that
	// everybody left.
	watchWindow = 10 * time.Second

	// A claim that never came back is handed out again. An agent that fell over
	// halfway must not take a question into the grave with it.
	claimPatience = 10 * time.Minute
)

// Comment is one thread: what was selected, what was asked, and everything
// said about it since.
type Comment struct {
	ID       string       `json:"id"`
	At       time.Time    `json:"at"`
	Status   string       `json:"status"` // open, thinking, answered
	Archived bool         `json:"archived,omitempty"`
	PickedUp *time.Time   `json:"pickedUpAt,omitempty"`
	Where    CommentWhere `json:"where"`
	Messages []Message    `json:"messages"`
}

// Message is one thing said. Prose, or the same blocks a step is written in:
// a question about a function is answered best by that function, at its own
// line numbers, with the button that opens it. The page already knows how to
// draw those, so an answer that uses them costs no renderer of its own.
type Message struct {
	From   string    `json:"from"` // reader or agent
	At     time.Time `json:"at"`
	Text   string    `json:"text,omitempty"`
	Blocks []Block   `json:"blocks,omitempty"`
}

func (m Message) empty() bool { return strings.TrimSpace(m.Text) == "" && len(m.Blocks) == 0 }

// CommentWhere is enough to put the thread back beside the words it is about,
// and enough for somebody with the checkout to go and look. Lines are the
// file's own numbering, because that is the number an editor opens on.
type CommentWhere struct {
	Step  string        `json:"step"`
	Title string        `json:"title,omitempty"`
	Block string        `json:"block,omitempty"`
	Kind  string        `json:"kind,omitempty"` // code or text
	File  string        `json:"file,omitempty"`
	Lines *CommentLines `json:"lines,omitempty"`
	Quote string        `json:"quote,omitempty"`
	Start int           `json:"start,omitempty"`
}

type CommentLines struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// CommentsView is what a caller gets, and it is the same shape whether it
// arrives with the walkthrough or on its own.
type CommentsView struct {
	Rev      int        `json:"rev"`
	Watching bool       `json:"watching"`
	Prompt   string     `json:"prompt,omitempty"`
	Threads  []*Comment `json:"threads"`
}

type commentFile struct {
	Version     string     `json:"version"`
	Walkthrough string     `json:"walkthrough"`
	Title       string     `json:"title,omitempty"`
	Next        int        `json:"next"`
	Threads     []*Comment `json:"threads"`
}

func (c *Comment) clone() *Comment {
	out := *c
	out.Messages = append([]Message(nil), c.Messages...)
	if c.PickedUp != nil {
		at := *c.PickedUp
		out.PickedUp = &at
	}
	if c.Where.Lines != nil {
		lines := *c.Where.Lines
		out.Where.Lines = &lines
	}
	return &out
}

// needsReply is what a watcher is handed. Anything the reader said last is
// waiting on an answer, and so is a claim that has been sitting there long
// enough that whoever took it is not coming back.
func (c *Comment) needsReply(now time.Time) bool {
	if c.Archived {
		return false
	}
	if c.Status == "thinking" {
		return c.PickedUp == nil || now.Sub(*c.PickedUp) > claimPatience
	}
	return c.Status != "answered"
}

/*
An answer in blocks is held to the shape a step is held to, and to nothing

	else. The rules `inspect2` adds on top are about a document somebody
	publishes: that a snippet's hash matches the file it names, that every id
	is unique across the document. A reply is neither published nor part of the
	document, and asking an agent to sha256 the three lines it is pointing at
	would buy nothing.
*/
var blockSchema = sync.OnceValue(func() *schemaDoc {
	full := schemaFor(FormatV2)
	if full == nil {
		return nil
	}
	root := map[string]any{
		"type":  "array",
		"items": map[string]any{"$ref": "#/$defs/block"},
	}
	if defs, ok := full.root["$defs"]; ok {
		root["$defs"] = defs
	}
	return &schemaDoc{root: root, pats: full.pats}
})

// readBlocks turns what was sent into blocks, or says what is wrong with it in
// the same words cw check uses on a walkthrough.
func readBlocks(raw json.RawMessage) ([]Block, []string) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, []string{err.Error()}
	}
	sch := blockSchema()
	if sch == nil {
		return nil, []string{"this build has no schema to check blocks against"}
	}
	if errs := sch.Validate(generic); len(errs) > 0 {
		return nil, errs
	}
	var out []Block
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, []string{err.Error()}
	}
	if len(out) == 0 {
		return nil, []string{"an answer with no blocks in it is not an answer"}
	}
	return out, nil
}

// ---------------------------------------------------------------- the store

type commentStore struct {
	mu   sync.Mutex
	path string
	rev  int
	seen time.Time // the last heartbeat from a watcher
	file commentFile
}

// commentsDir is where the threads live: beside the settings rather than beside
// the walkthrough. cw open serves a copy out of a temp directory, and a file
// written next to somebody's walkthrough in their own repository is a file that
// gets committed by accident. A var so a test can point it somewhere else.
var commentsDir = func() (string, error) {
	p, err := settingsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "comments"), nil
}

// commentKey names the file. A published walkthrough is known by its slug, so
// the questions asked about it are still there tomorrow; a local file is known
// by its name plus a slice of the hash of its path, because two walkthroughs
// called walkthrough.json are not the same walkthrough.
func commentKey(slugHint, file string) string {
	if slugHint != "" {
		return slug(slugHint)
	}
	stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	p := filepath.Clean(file)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	sum := sha256.Sum256([]byte(p))
	return slug(stem) + "-" + hex.EncodeToString(sum[:4])
}

func openComments(key, title string) *commentStore {
	c := &commentStore{file: commentFile{
		Version: commentsFormat, Walkthrough: key, Title: title, Next: 1}}
	dir, err := commentsDir()
	if err != nil {
		// Without a settings directory the threads live for as long as the
		// server does. That is worth having; failing to start is not.
		return c
	}
	c.path = filepath.Join(dir, key+".json")

	var read commentFile
	if err := readJSONFile(c.path, &read); err != nil || read.Version != commentsFormat {
		return c
	}
	// The title follows the walkthrough rather than the record: a republished
	// walkthrough with a new title is still the same walkthrough.
	read.Walkthrough, read.Title = key, title
	if read.Next < len(read.Threads)+1 {
		read.Next = len(read.Threads) + 1
	}
	c.file = read
	return c
}

// save runs under the lock, and a failure to write is a line on stderr rather
// than a refusal: the reader is mid-sentence and the thread is already in
// memory.
func (c *commentStore) save() {
	c.rev++
	if c.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cw: cannot keep comments in %s: %v\n", filepath.Dir(c.path), err)
		return
	}
	if err := writeJSONFile(c.path, c.file); err != nil {
		fmt.Fprintf(os.Stderr, "cw: cannot write %s: %v\n", c.path, err)
	}
}

func (c *commentStore) view(prompt string) CommentsView {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := CommentsView{Rev: c.rev, Watching: time.Since(c.seen) < watchWindow, Prompt: prompt}
	for _, t := range c.file.Threads {
		out.Threads = append(out.Threads, t.clone())
	}
	if out.Threads == nil {
		out.Threads = []*Comment{}
	}
	return out
}

func (c *commentStore) add(where CommentWhere, text string) *Comment {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	t := &Comment{
		ID:     strconv.Itoa(c.file.Next),
		At:     now,
		Status: "open",
		Where:  where,
		Messages: []Message{{
			From: "reader", At: now, Text: text}},
	}
	c.file.Next++
	c.file.Threads = append(c.file.Threads, t)
	c.save()
	return t.clone()
}

func (c *commentStore) find(id string) *Comment {
	for _, t := range c.file.Threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// reply appends to a thread from either end. The reader asking again reopens
// it, which is what makes a second question a second question rather than a
// note nobody is waiting on.
func (c *commentStore) reply(id string, m Message) (*Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.find(id)
	if t == nil {
		return nil, fmt.Errorf("there is no comment %s", id)
	}
	from := m.From
	m.At = time.Now().UTC()
	t.Messages = append(t.Messages, m)
	if from == "agent" {
		t.Status = "answered"
	} else {
		t.Status = "open"
		t.PickedUp = nil
	}
	c.save()
	return t.clone(), nil
}

// next is the whole watch loop on the server's side: it is the heartbeat that
// says somebody is listening, and it hands over one thread at a time.
func (c *commentStore) next() *Comment {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.seen = now
	for _, t := range c.file.Threads {
		if !t.needsReply(now) {
			continue
		}
		at := now.UTC()
		t.Status, t.PickedUp = "thinking", &at
		c.save()
		return t.clone()
	}
	return nil
}

// archive puts one away and takes it back out. Nothing here removes a thread:
// what somebody asked is a record of a reading, and the answer under it is
// often the most useful thing on the step. The file is a file, for the day
// somebody really does want one gone.
func (c *commentStore) archive(id string, on bool) (*Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.find(id)
	if t == nil {
		return nil, fmt.Errorf("there is no comment %s", id)
	}
	t.Archived = on
	c.save()
	return t.clone(), nil
}

func (c *commentStore) state() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rev, time.Since(c.seen) < watchWindow
}

// watchPrompt is what the page puts on the clipboard when nothing is listening.
// The server writes it because it is the only one that knows what this
// walkthrough is called on the command line.
func watchPrompt(key, title string) string {
	name := title
	if name == "" {
		name = key
	}
	return fmt.Sprintf(`I am reading the walkthrough "%s" and leaving comments in it.
Answer them while I read:

  cw comments watch %s --json

That waits until a comment comes in, prints it, and stops. Answer it with
cw comments reply <id> --file <a file with the answer in it>, then start the
watch again. Keep going until I say to stop.`, name, key)
}

// ---------------------------------------------------------------- the record

// servedRun is one running cw serve, and enough about it for the command to
// reach it from another process.
type servedRun struct {
	PID   int       `json:"pid"`
	Port  int       `json:"port"`
	Token string    `json:"token"`
	Key   string    `json:"key"`
	Title string    `json:"title"`
	File  string    `json:"file"`
	At    time.Time `json:"at"`
}

// servingStore sits beside worktrees.json for the same reason: it is a record
// of what a run is doing, not a secret and not about one walkthrough. The token
// in it is the per-run value already stamped into the page's HTML, it is only
// worth anything on 127.0.0.1, and without it the command cannot pass the guard
// that keeps other pages out. The file is written 0600 all the same.
var servingStore = func() (string, error) {
	p, err := settingsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "servers.json"), nil
}

var servingMu sync.Mutex

func readRuns() []servedRun {
	p, err := servingStore()
	if err != nil {
		return nil
	}
	var all []servedRun
	if err := readJSONFile(p, &all); err != nil {
		return nil
	}
	return all
}

func writeRuns(all []servedRun) error {
	p, err := servingStore()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := writeJSONFile(p, all); err != nil {
		return err
	}
	return os.Chmod(p, 0o600)
}

func rememberRun(r servedRun) {
	servingMu.Lock()
	defer servingMu.Unlock()
	kept := []servedRun{}
	for _, old := range readRuns() {
		if old.PID != r.PID {
			kept = append(kept, old)
		}
	}
	if err := writeRuns(append(kept, r)); err != nil {
		fmt.Fprintf(os.Stderr, "cw: cannot record this run, so cw comments will not find it: %v\n", err)
	}
}

func forgetRun(pid int) {
	servingMu.Lock()
	defer servingMu.Unlock()
	kept := []servedRun{}
	for _, old := range readRuns() {
		if old.PID != pid {
			kept = append(kept, old)
		}
	}
	_ = writeRuns(kept)
}

// liveRuns drops whatever no longer answers. A run that was killed leaves its
// line behind, and a port that has since been taken by something else does not
// know this token, so both come out the same way.
func liveRuns() []servedRun {
	all := readRuns()
	live := []servedRun{}
	for _, r := range all {
		if _, err := callRun(r, http.MethodGet, "/api/state", nil, 400*time.Millisecond); err == nil {
			live = append(live, r)
		}
	}
	if len(live) != len(all) {
		servingMu.Lock()
		// Re-read under the lock: a run that started while this was probing has
		// a line of its own that never went past the probe.
		kept := []servedRun{}
		for _, r := range readRuns() {
			for _, ok := range live {
				if ok.PID == r.PID {
					kept = append(kept, r)
					break
				}
			}
		}
		_ = writeRuns(kept)
		servingMu.Unlock()
	}
	return live
}

func callRun(r servedRun, method, path string, body any, timeout time.Duration) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", r.Port, path)
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Cw-Token", r.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, errors.New(strings.TrimSpace(string(raw)))
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- the routes

func (s *server) commentsView() CommentsView {
	return s.comments.view(watchPrompt(s.key, s.title))
}

// commentRev is read under the lock like everything else on the store: the
// page polling and a watcher taking a thread are two goroutines.
func (s *server) commentRev() int {
	rev, _ := s.comments.state()
	return rev
}

func (s *server) handleComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.commentsView())
	case http.MethodPost:
		var in struct {
			Where CommentWhere `json:"where"`
			Text  string       `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		in.Text = strings.TrimSpace(in.Text)
		if in.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a comment with nothing in it says nothing"})
			return
		}
		t := s.comments.add(in.Where, in.Text)
		fmt.Printf("a comment on %s: %s\n", where(t), oneLine(in.Text))
		writeJSON(w, http.StatusOK, map[string]any{"thread": t, "rev": s.commentRev()})
	default:
		http.Error(w, "use GET or POST", http.StatusMethodNotAllowed)
	}
}

// handleComment is everything with an id on the end, plus the one reserved
// name: /api/comments/next is what a watcher asks, and ids are numbers, so it
// can never be one of them.
func (s *server) handleComment(w http.ResponseWriter, r *http.Request) {
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/comments/"), "/")
	id, action, _ := strings.Cut(tail, "/")
	if action == "archive" {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		t, err := s.comments.archive(id, in.On)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"thread": t, "rev": s.commentRev()})
		return
	}
	if id == "next" {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		out := map[string]any{
			"walkthrough": s.key, "title": s.title,
			"root": filepath.ToSlash(s.root), "file": filepath.ToSlash(s.file),
		}
		if t := s.comments.next(); t != nil {
			out["thread"] = t
			fmt.Printf("an agent picked up comment %s\n", t.ID)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var in struct {
			From   string          `json:"from"`
			Text   string          `json:"text"`
			Blocks json.RawMessage `json:"blocks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		said := Message{From: "reader", Text: strings.TrimSpace(in.Text)}
		if in.From == "agent" {
			said.From = "agent"
		}
		if len(in.Blocks) > 0 {
			blocks, errs := readBlocks(in.Blocks)
			if len(errs) > 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "those blocks are not a step's blocks", "errors": errs})
				return
			}
			said.Blocks, said.Text = blocks, ""
		}
		if said.empty() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "an empty reply is not a reply"})
			return
		}
		t, err := s.comments.reply(id, said)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if said.From == "agent" {
			fmt.Printf("comment %s answered\n", id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"thread": t, "rev": s.commentRev()})
	default:
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------- the command

const commentsUsage = `cw comments [<file, url or name>]              what is open, oldest first
cw comments watch [<target>] [--for 10m]      wait for one to answer, and take it
cw comments reply <id> [--text T | --file F]  answer it, or read the answer on stdin
                       [--json]               on the first two, for something reading this
`

func cmdComments(args []string) {
	verb := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "watch", "reply", "list":
			verb, args = args[0], args[1:]
		}
	}

	target, id, text, file := "", "", "", ""
	asJSON, stdin := false, false
	patience := 10 * time.Minute
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("%s needs a value", a)
			}
			i++
			return args[i]
		}
		switch {
		case a == "--json":
			asJSON = true
		case a == "--text":
			text = next()
		case strings.HasPrefix(a, "--text="):
			text = strings.TrimPrefix(a, "--text=")
		case a == "--file":
			file = next()
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case a == "--for":
			patience = duration(next())
		case strings.HasPrefix(a, "--for="):
			patience = duration(strings.TrimPrefix(a, "--for="))
		case a == "-":
			stdin = true
		case a == "-h", a == "--help":
			fmt.Print(commentsUsage)
			return
		case strings.HasPrefix(a, "-"):
			die("unknown flag %q", a)
		case verb == "reply" && id == "":
			id = a
		case target == "":
			target = a
		default:
			die("one walkthrough at a time, got %q and %q", target, a)
		}
	}

	run := pickRun(target)
	switch verb {
	case "watch":
		watchComments(run, patience, asJSON)
	case "reply":
		replyToComment(run, id, text, file, stdin)
	default:
		listComments(run, asJSON)
	}
}

// pickRun works out which running cw is meant. With nothing to go on that is
// the only one there is, because asking a question about a walkthrough nobody
// is reading is not a thing anyone means to do.
func pickRun(target string) servedRun {
	runs := liveRuns()
	if len(runs) == 0 {
		die("nothing is being served right now. Start it with cw open <name> or cw serve <file>")
	}
	if target == "" {
		if len(runs) == 1 {
			return runs[0]
		}
		var names []string
		for _, r := range runs {
			names = append(names, r.Key)
		}
		die("%d walkthroughs are being served, so name one: %s", len(runs), strings.Join(names, ", "))
	}

	want := target
	if abs, err := filepath.Abs(target); err == nil {
		if _, err := os.Stat(abs); err == nil {
			want = commentKey("", abs)
		}
	}
	if strings.Contains(want, "/") {
		want = want[strings.LastIndexByte(want, '/')+1:]
	}
	want = strings.TrimSuffix(want, ".json")
	for _, r := range runs {
		if r.Key == want || r.Key == slug(want) || samePath(r.File, target) {
			return r
		}
	}
	var names []string
	for _, r := range runs {
		names = append(names, r.Key)
	}
	die("nothing called %q is being served. What is: %s", target, strings.Join(names, ", "))
	return servedRun{}
}

func watchComments(run servedRun, patience time.Duration, asJSON bool) {
	chat := os.Stdout
	if asJSON {
		chat = os.Stderr
	}
	fmt.Fprintf(chat, "waiting for a comment on %s, %s at most\n", run.Key, patience)

	deadline := time.Now().Add(patience)
	for {
		out, err := callRun(run, http.MethodPost, "/api/comments/next", map[string]any{}, 5*time.Second)
		if err != nil {
			die("%v", err)
		}
		if raw, ok := out["thread"]; ok && raw != nil {
			var t Comment
			if err := remarshal(raw, &t); err != nil {
				die("%v", err)
			}
			if asJSON {
				printJSON(map[string]any{
					"walkthrough": out["walkthrough"], "title": out["title"],
					"root": out["root"], "file": out["file"], "comment": &t,
				})
				return
			}
			fmt.Printf("\n%s\n", strings.TrimRight(describeComment(&t), "\n"))
			if root, _ := out["root"].(string); root != "" {
				fmt.Printf("  the code is in %s\n", root)
			}
			fmt.Printf("\nanswer it with cw comments reply %s --file <a file with the answer in it>\n", t.ID)
			return
		}
		if time.Now().After(deadline) {
			if asJSON {
				printJSON(map[string]any{"walkthrough": run.Key, "comment": nil})
			} else {
				fmt.Printf("nothing came in. Start the watch again to keep listening\n")
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func replyToComment(run servedRun, id, text, file string, stdin bool) {
	if id == "" {
		die("which comment? cw comments reply <id>")
	}
	switch {
	case file != "":
		raw, err := os.ReadFile(file)
		if err != nil {
			die("%v", err)
		}
		text = string(raw)
	case text == "" || stdin:
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			die("%v", err)
		}
		text = string(raw)
	}
	if strings.TrimSpace(text) == "" {
		die("there is nothing in that answer")
	}

	// An answer that parses as a list of blocks is one, and anything else is
	// prose. Markdown that opens with a link is still markdown: [text](url) is
	// not JSON, and it is the JSON that has to prove itself here.
	body := map[string]any{"from": "agent", "text": text}
	if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "[") {
		var probe []map[string]any
		if json.Unmarshal([]byte(trimmed), &probe) == nil {
			body = map[string]any{"from": "agent", "blocks": json.RawMessage(trimmed)}
		}
	}
	if _, err := callRun(run, http.MethodPost, "/api/comments/"+id, body, 5*time.Second); err != nil {
		die("%v", err)
	}
	if _, blocks := body["blocks"]; blocks {
		fmt.Printf("answered comment %s on %s, in blocks\n", id, run.Key)
	} else {
		fmt.Printf("answered comment %s on %s\n", id, run.Key)
	}
}

func listComments(run servedRun, asJSON bool) {
	out, err := callRun(run, http.MethodGet, "/api/comments", nil, 5*time.Second)
	if err != nil {
		die("%v", err)
	}
	var view CommentsView
	if err := remarshal(out, &view); err != nil {
		die("%v", err)
	}
	if asJSON {
		printJSON(map[string]any{"walkthrough": run.Key, "title": run.Title,
			"watching": view.Watching, "threads": view.Threads})
		return
	}
	fmt.Printf("%s\n", run.Title)
	fmt.Printf("  walkthrough  %s\n", run.Key)
	fmt.Printf("  url          http://127.0.0.1:%d/\n", run.Port)
	if len(view.Threads) == 0 {
		fmt.Printf("\nnobody has asked anything yet\n")
		return
	}
	fmt.Println()
	for _, t := range view.Threads {
		fmt.Print(describeComment(t))
	}
}

// describeComment is the same thread a page shows, written out for a terminal:
// where it hangs, what was selected, and everything said about it so far.
func describeComment(t *Comment) string {
	var b strings.Builder
	state := t.Status
	if t.Archived {
		state += ", archived"
	}
	fmt.Fprintf(&b, "  #%s  %s  [%s]\n", t.ID, where(t), state)
	if t.Where.File != "" {
		fmt.Fprintf(&b, "      %s\n", fileAndLines(t.Where))
	}
	for _, line := range strings.Split(strings.TrimRight(t.Where.Quote, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(&b, "      | %s\n", line)
		}
	}
	for _, m := range t.Messages {
		who := "reader"
		if m.From == "agent" {
			who = "agent "
		}
		for i, line := range said(m) {
			if i == 0 {
				fmt.Fprintf(&b, "      %s  %s\n", who, line)
			} else {
				fmt.Fprintf(&b, "              %s\n", line)
			}
		}
	}
	return b.String()
}

// said is what a message reads as in a terminal. Blocks are named rather than
// printed: a diff and a timeline are drawings, and the page is where they are
// worth looking at.
func said(m Message) []string {
	if len(m.Blocks) == 0 {
		return strings.Split(strings.TrimRight(m.Text, "\n"), "\n")
	}
	var out []string
	for _, b := range m.Blocks {
		out = append(out, "["+b.Type+"] "+oneLine(blockGist(b)))
	}
	return out
}

func blockGist(b Block) string {
	switch {
	case b.Snippet != nil && b.Snippet.Source != nil:
		return fmt.Sprintf("%s %d-%d", b.Snippet.Source.File,
			b.Snippet.Source.StartLine, b.Snippet.Source.EndLine)
	case b.Snippet != nil:
		return b.Snippet.Label
	case b.Alt != "":
		return b.Alt
	case b.Title != "":
		return b.Title
	case b.Caption != "":
		return b.Caption
	case b.Name != "":
		return b.Name
	}
	return b.Text
}

func where(t *Comment) string {
	if t.Where.Title != "" {
		return t.Where.Title
	}
	if t.Where.Step != "" {
		return t.Where.Step
	}
	return "the walkthrough"
}

func fileAndLines(w CommentWhere) string {
	if w.Lines == nil {
		return w.File
	}
	if w.Lines.End > w.Lines.Start {
		return fmt.Sprintf("%s %d-%d", w.File, w.Lines.Start, w.Lines.End)
	}
	return fmt.Sprintf("%s %d", w.File, w.Lines.Start)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " ..."
	}
	if len(s) > 90 {
		s = s[:90] + " ..."
	}
	return s
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// remarshal moves a decoded map into the struct it should have been, which is
// what a client that only ever reads a handful of fields would otherwise do by
// hand for each of them.
func remarshal(from any, into any) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// duration takes the short forms somebody types at a prompt and nothing else. A
// bare number is minutes, because that is the unit this is measured in.
func duration(s string) time.Duration {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		die("%q is not a length of time. Try 30s, 10m or 2h", s)
	}
	return d
}
