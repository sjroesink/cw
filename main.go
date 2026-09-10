package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed all:web
var webFS embed.FS

//go:embed schema/walkthrough.schema.json
var schemaV1JSON []byte

//go:embed schema/walkthrough.v2.schema.json
var schemaV2JSON []byte

const usageText = `cw: serve a code walkthrough as a page you can step through.

  cw serve <walkthrough.json> [flags]   open it in a browser
  cw check <walkthrough.json> [flags]   validate against the schema, no server
  cw migrate <walkthrough.json>         fill in the ids and anchors a file is missing
  cw migrate --to cw/2 <file>           lift a cw/1 file, and say what it could not work out
             [--assume-diff-start]      take a cw/1 diff's one line number for both sides
  cw schema [--write]                   print the JSON schema, or write a copy to point at
  cw ides                               list the editors found on this machine
  cw settings [--path]                  print the settings file
  cw cache warm | clear                 fetch the mermaid and typeface bundle, or drop it

Sharing one:
  cw publish <walkthrough.json> [--site URL] [--slug NAME] [--new] [--force]
             [--password PW | --no-password] [--allow CIDR,... | --no-allow]
  cw open <url or name> [--root DIR]    read a published one with the local buttons
          [--worktree | --no-worktree]  add or move a worktree for its commit without asking, or never
  cw worktrees [clean]                  the worktrees cw open added, and removing them

Answering what a reader asks, while one is being served:
  cw comments [<file, url or name>]     what has been asked, oldest first
  cw comments watch [target]            wait for one to answer, and take it
              [--for 10m]               how long to wait before giving up
  cw comments reply <id> [--text T]     answer it, or --file F, or - for stdin

Serving the site rather than one file:
  cw host [--addr :8080] [--data DIR] [--base-url URL]
  cw keys add <name> | list | rm <name>  [--data DIR]

Flags for serve and check:
  --root DIR     the checkout the file paths are relative to
  --port N       serve on this port (serve only, default: a free one)
  --no-open      do not launch a browser (serve only)
  --offline      never reach for the network, use the asset cache only
  --dev          read the web assets from disk instead of the binary
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usageText)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "host":
		cmdHost(os.Args[2:])
	case "keys":
		cmdKeys(os.Args[2:])
	case "publish":
		cmdPublish(os.Args[2:])
	case "open":
		cmdOpen(os.Args[2:])
	case "worktrees":
		cmdWorktrees(os.Args[2:])
	case "comments":
		cmdComments(os.Args[2:])
	case "schema":
		cmdSchema(os.Args[2:])
	case "ides":
		cmdIDEs()
	case "settings":
		cmdSettings(os.Args[2:])
	case "cache":
		cmdCache(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}
}

type flags struct {
	file    string
	slug    string
	root    string
	port    int
	noOpen  bool
	offline bool
	dev     bool
}

func parseFlags(args []string, wantFile bool) flags {
	f := flags{port: -1}
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
		case a == "--root":
			f.root = next()
		case strings.HasPrefix(a, "--root="):
			f.root = strings.TrimPrefix(a, "--root=")
		case a == "--port":
			f.port = atoiOr(next(), -1)
		case strings.HasPrefix(a, "--port="):
			f.port = atoiOr(strings.TrimPrefix(a, "--port="), -1)
		case a == "--no-open":
			f.noOpen = true
		case a == "--offline":
			f.offline = true
		case a == "--dev":
			f.dev = true
		case strings.HasPrefix(a, "-"):
			die("unknown flag %q", a)
		default:
			if f.file != "" {
				die("give one walkthrough file, got %q and %q", f.file, a)
			}
			f.file = a
		}
	}
	if wantFile && f.file == "" {
		die("give a walkthrough file")
	}
	return f
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "cw: "+format+"\n", a...)
	// Giving up is one of the ways a run ends, so a worktree this one added is
	// handed back here too, and the line that says this run is servable goes
	// with it. Every run that has neither notices nothing.
	forgetRun(os.Getpid())
	releaseWorktree()
	os.Exit(2)
}

// Both versions of the format are contracts in their own right, and they are
// read once because parsing a schema per document would be the same work over
// and over. A schema that will not load is a broken build rather than a broken
// walkthrough, so this dies rather than returning an error nobody can act on.
var schemas = sync.OnceValue(func() map[string]*schemaDoc {
	out := map[string]*schemaDoc{}
	for version, raw := range map[string][]byte{FormatV1: schemaV1JSON, FormatV2: schemaV2JSON} {
		sch, err := loadSchema(raw)
		if err != nil {
			die("%s: %v", version, err)
		}
		out[version] = sch
	}
	return out
})

// schemaFor picks the contract a document is read against. An unknown version
// comes back nil, because that is one sentence to an author rather than two
// hundred about fields that were never meant to be there.
func schemaFor(version string) *schemaDoc { return schemas()[version] }

func schemaBytes(version string) []byte {
	if version == FormatV1 {
		return schemaV1JSON
	}
	return schemaV2JSON
}

// resolveRoot decides which checkout the paths hang off: what was asked for,
// what the walkthrough says, or the repository the walkthrough sits in. An
// empty root is allowed: without one the page still reads, it just cannot open
// anything or say whether the code is still there.
func resolveRoot(f flags, hint string) string {
	pick := func(p, base string) string {
		if p == "" {
			return ""
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return ""
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return ""
		}
		return abs
	}
	cwd, _ := os.Getwd()
	dir := cwd
	if f.file != "" {
		if abs, err := filepath.Abs(f.file); err == nil {
			dir = filepath.Dir(abs)
		}
	}
	if r := pick(f.root, cwd); r != "" {
		return r
	}
	if r := pick(hint, dir); r != "" {
		return r
	}
	if out, err := runIn(dir, "git", "rev-parse", "--show-toplevel"); err == nil {
		if r := pick(strings.TrimSpace(out), dir); r != "" {
			return r
		}
	}
	return ""
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func rootName(root string) string {
	if root == "" {
		return ""
	}
	if out, err := runIn(root, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(out); b != "" && b != "HEAD" {
			return filepath.Base(root) + " @ " + b
		}
	}
	return filepath.Base(root)
}

// ---------------------------------------------------------------- check

func cmdCheck(args []string) {
	f := parseFlags(args, true)
	res, err := LoadDoc(f.file)
	if err != nil {
		die("%v", err)
	}
	view := res.View()
	root := resolveRoot(f, view.RootHint)
	tree := &Tree{Root: root}
	checked, moved, stale := tree.Verify(view)

	fmt.Printf("%s\n", view.Title)
	fmt.Printf("  format %s, parts %d, steps %d\n", view.Format, len(view.Tour()), view.Steps)
	if root == "" {
		fmt.Printf("  root   none, so nothing was checked against a working tree\n")
	} else {
		fmt.Printf("  root   %s\n", root)
	}
	fmt.Println()

	for pi, p := range view.Tour() {
		fmt.Printf("  [%d] %s\n", pi+1, p.Title)
		for _, sec := range p.Sections {
			fmt.Printf("      %s\n", sec.Title)
			for _, snip := range sec.Snippets {
				report("        ", snip.Name, snip.State, snip.Note)
			}
		}
	}

	if len(res.Errors) > 0 {
		fmt.Printf("\n%d error(s):\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}
	if len(res.Warnings) > 0 {
		fmt.Printf("\n%d warning(s):\n", len(res.Warnings))
		for _, w := range res.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
	fmt.Printf("\n%d snippet(s) checked against the tree: %d moved, %d no longer there\n", checked, moved, stale)
	if len(res.Errors) > 0 {
		os.Exit(1)
	}
}

// report prints what the working tree said about one snippet. The states are
// the document's own words and the two versions do not use the same ones, so
// both sets are read here rather than translated into a third.
func report(indent, what, state, note string) {
	switch state {
	case "":
		return
	case "ok", "match":
		fmt.Printf("%sok     %s\n", indent, what)
	case "unchecked":
		fmt.Printf("%s-      %s\n", indent, what)
	case "moved":
		fmt.Printf("%sMOVED  %s, %s\n", indent, what, note)
	default:
		if note == "" {
			note = state
		}
		fmt.Printf("%sSTALE  %s, %s\n", indent, what, note)
	}
}

// ---------------------------------------------------------------- migrate

// cmdMigrate rewrites the file in the shape this build reads: cw/1, with the
// pull request in one source block, and with the ids and snippet anchors filled
// in. It never touches an id that is already there, because a reader's saved
// progress is keyed on them.
func cmdMigrate(args []string) {
	to, opt, rest := FormatV1, LiftOptions{}, []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--to":
			if i+1 >= len(args) {
				die("--to needs a version, and the only one worth migrating to is %s", FormatV2)
			}
			i++
			to = args[i]
		case strings.HasPrefix(a, "--to="):
			to = strings.TrimPrefix(a, "--to=")
		case a == "--assume-diff-start":
			opt.AssumeDiffStart = true
		default:
			rest = append(rest, a)
		}
	}
	if to != FormatV1 && to != FormatV2 {
		die("this build migrates to %s and %s, not to %q", FormatV1, FormatV2, to)
	}

	f := parseFlags(rest, true)
	res, err := LoadDoc(f.file)
	if err != nil {
		die("%v", err)
	}
	if len(res.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "%d error(s), and none of them are fixed by migrating:\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		os.Exit(1)
	}

	ids, anchors := 0, 0
	var todo []string
	var out any

	switch {
	case to == FormatV2 && res.Doc != nil:
		lifted, gaps := LiftToV2(res.Doc, opt)
		anchors, todo, out = EnsureAnchors2(lifted), gaps, lifted
	case res.Doc2 != nil:
		// cw/2 requires ids in the file, so there are never any to fill in: the
		// schema refused the document before it got here.
		anchors, out = EnsureAnchors2(res.Doc2), res.Doc2
	default:
		ids, anchors, out = EnsureIDs(res.Doc), EnsureAnchors(res.Doc), res.Doc
	}

	if err := WriteDoc(f.file, out); err != nil {
		die("%v", err)
	}
	if to == FormatV2 && res.Doc != nil {
		fmt.Printf("%s is now %s\n", f.file, FormatV2)
	} else {
		fmt.Printf("%s is now %s\n", f.file, res.View().Format)
	}
	fmt.Printf("  %d id(s) filled in, %d snippet anchor(s) filled in\n", ids, anchors)

	// The list is the point of the command. Everything above it is what could be
	// worked out; everything below it is what somebody has to decide, and it was
	// left out rather than guessed at.
	if len(todo) > 0 {
		fmt.Printf("\n%d thing(s) this could not work out on its own:\n", len(todo))
		for i, t := range todo {
			fmt.Printf("  %d. %s\n", i+1, t)
		}
		fmt.Printf("\nRun cw check on it to see which of them the schema refuses.\n")
	}
}

// WriteDoc writes a walkthrough back over itself, indented the way a hand-edited
// file is and with the schema reference kept at the top.
// WriteDoc writes a walkthrough back out in the form somebody will edit it in.
// The encoder is built by hand rather than using json.MarshalIndent for one
// reason: MarshalIndent escapes <, > and & into \u003c and friends, which is
// correct JSON and unreadable prose, and a walkthrough is full of prose about
// code that contains all three.
func WriteDoc(path string, d any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// ---------------------------------------------------------------- small commands

// The schema lives in the binary, so the honest answer to "where is it" is a
// copy on disk the author can point their editor at.
func cmdSchema(args []string) {
	version := FormatDefault
	if len(args) > 0 && args[0] == "--v1" {
		version, args = FormatV1, args[1:]
	}
	if len(args) == 0 {
		fmt.Print(string(schemaBytes(version)))
		return
	}
	if args[0] != "--write" {
		die("the schema flags are --v1 and --write")
	}
	sp, err := settingsPath()
	if err != nil {
		die("%v", err)
	}
	name := "walkthrough.schema.json"
	if version == FormatV1 {
		name = "walkthrough.v1.schema.json"
	}
	out := filepath.Join(filepath.Dir(sp), name)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		die("%v", err)
	}
	if err := os.WriteFile(out, schemaBytes(version), 0o644); err != nil {
		die("%v", err)
	}
	fmt.Println(out)
}

func cmdIDEs() {
	s, path, _ := LoadSettings()
	fmt.Printf("settings: %s\nconfigured: %s\n\n", path, s.IDE)
	for _, ide := range DetectIDEs() {
		mark := "  "
		if ide.Available {
			mark = "* "
		}
		note := ""
		if ide.Hinted {
			note = "   (this terminal is running inside it)"
		}
		if ide.NoLine {
			note += "   (opens the file, not the line)"
		}
		fmt.Printf("%s%-16s %-24s %s%s\n", mark, ide.ID, ide.Name, ide.Path, note)
	}
	fmt.Printf("\nauto would pick: %s\n", firstAvailableIDE())
}

func cmdSettings(args []string) {
	s, path, err := LoadSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cw: %v\n", err)
	}
	if len(args) > 0 && args[0] == "--path" {
		fmt.Println(path)
		return
	}
	body, _ := json.MarshalIndent(s, "", "  ")
	fmt.Printf("%s\n%s\n", path, body)
}

func cmdCache(args []string) {
	if len(args) == 0 {
		die("the cache commands are: cw cache warm, cw cache clear")
	}
	v := NewVendor(false)
	switch args[0] {
	case "clear":
		n, err := v.Clear()
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("removed %d file(s) from %s\n", n, v.Dir)
	case "warm":
		// What a container build runs, so the image ships with the assets and
		// the running container never has to reach the network.
		got, err := v.Warm()
		for _, name := range got {
			fmt.Printf("  %s\n", name)
		}
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("%d file(s) cached in %s\n", len(got), v.Dir)
	default:
		die("the cache commands are: cw cache warm, cw cache clear")
	}
}

// ---------------------------------------------------------------- serve

type server struct {
	file   string
	root   string
	dev    bool
	token  string
	vendor *Vendor
	web    fs.FS

	// What a reader asked while reading this one. Local only: the hosted
	// server has no store, no routes for it and no agent to answer with.
	key      string
	title    string
	comments *commentStore
}

type payload struct {
	// The document goes out as the bytes it already is. The page picks its
	// renderer from the version inside it, and re-encoding it through one
	// version's struct would be the server deciding that on its behalf.
	Doc      json.RawMessage `json:"doc"`
	Root     string          `json:"root"`
	RootName string          `json:"rootName"`
	File     string          `json:"file"`
	Settings Settings        `json:"settings"`
	SetPath  string          `json:"settingsPath"`
	IDEs     []IDE           `json:"ides"`
	Errors   []string        `json:"errors,omitempty"`
	Warnings []string        `json:"warnings,omitempty"`
	Moved    int             `json:"moved"`
	Stale    int             `json:"stale"`
	Stamp    string          `json:"stamp"`

	// Set by the local server only, for the same reason the open buttons are:
	// there is nobody on the other end of a comment on a page nobody is
	// serving from a terminal.
	Comments *CommentsView `json:"comments,omitempty"`

	// Set by the hosted server only. The page reads Hosted to decide whether a
	// line number opens an editor or a link, and everything below it is the
	// answer to what it can say without a working tree in front of it.
	Hosted bool         `json:"hosted,omitempty"`
	Meta   *Meta        `json:"meta,omitempty"`
	GitHub *GitHubLinks `json:"github,omitempty"`
}

func cmdServe(args []string) {
	runServe(parseFlags(args, true))
}

// runServe is the local reader, split out from the command so cw open can point
// it at a walkthrough it just fetched instead of one on disk.
func runServe(f flags) {
	abs, err := filepath.Abs(f.file)
	if err != nil {
		die("%v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		die("cannot read %s: %v", f.file, err)
	}
	settings, spath, serr := LoadSettings()
	if serr != nil {
		fmt.Fprintf(os.Stderr, "cw: %v\n", serr)
	}
	if f.offline {
		settings.Offline = true
	}

	res, err := LoadDoc(abs)
	if err != nil {
		die("%v", err)
	}
	root := resolveRoot(f, res.View().RootHint)

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		die("the web assets are missing from this build: %v", err)
	}
	key := commentKey(f.slug, abs)
	s := &server{file: abs, root: root, dev: f.dev, token: randomToken(),
		vendor: NewVendor(settings.Offline), web: sub,
		key: key, title: res.View().Title, comments: openComments(key, res.View().Title)}
	s.vendor.Prewarm()

	port := settings.Port
	if f.port >= 0 {
		port = f.port
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		die("cannot listen on port %d: %v", port, err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)

	// cw comments runs in a second terminal and has to find this one. The
	// line goes when the run does, and one left behind by a kill is dropped
	// by whoever reads it next.
	rememberRun(servedRun{PID: os.Getpid(), Port: ln.Addr().(*net.TCPAddr).Port,
		Token: s.token, Key: key, Title: res.View().Title, File: abs, At: time.Now()})

	mux := s.routes()

	fmt.Printf("%s\n", res.View().Title)
	fmt.Printf("  url       %s\n", url)
	if root == "" {
		fmt.Printf("  root      none: the open buttons will say so\n")
	} else {
		fmt.Printf("  root      %s\n", root)
	}
	fmt.Printf("  editor    %s\n", describeIDE(settings))
	fmt.Printf("  settings  %s\n", spath)
	fmt.Printf("  comments  cw comments watch %s\n", key)
	if len(res.Errors) > 0 {
		fmt.Printf("\n%d error(s), shown in the page as well:\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}
	if len(res.Warnings) > 0 {
		fmt.Printf("\n%d warning(s):\n", len(res.Warnings))
		for _, w := range res.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
	fmt.Printf("\nctrl-c to stop\n")

	if settings.OpenBrowser && !f.noOpen {
		go func() {
			time.Sleep(180 * time.Millisecond)
			_ = openURL(url)
		}()
	}

	// A worktree cw open added for this walkthrough is a directory nobody asked
	// for, so it goes again when the reading stops. ctrl-c is how that happens,
	// which is why it is caught here rather than taking the process out where
	// it stands. Every other run has nothing to give back and notices nothing.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		fmt.Println()
		forgetRun(os.Getpid())
		releaseWorktree()
		os.Exit(0)
	}()

	if err := http.Serve(ln, mux); err != nil {
		die("%v", err)
	}
}

func describeIDE(s Settings) string {
	id := s.IDE
	if id == "" || id == "auto" {
		id = firstAvailableIDE()
		if id == "auto" {
			return "none found. Pick one under Settings in the page"
		}
		return id + " (detected)"
	}
	return id
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

/*
The page itself is served with no-store, but its stylesheet and its script were
not: they sit at a fixed path, so anything that cached them yesterday keeps
handing them out after a deploy. On cw.roesink.dev that is Cloudflare holding
app.js for four hours by default, which is four hours of a fresh page running
last week's code, and the two disagreeing in ways nobody can reproduce.

So the page asks for them by a stamp of what is in this build. A build that
changes a file changes the URL, which makes caching them hard the right thing
rather than a trap.
*/

var assetStamp = sync.OnceValue(func() string {
	sum := sha256.New()
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return "0"
	}
	_ = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(sub, path)
		if err != nil {
			return nil
		}
		fmt.Fprintf(sum, "%s:%d:", path, len(body))
		sum.Write(body)
		return nil
	})
	return hex.EncodeToString(sum.Sum(nil))[:12]
})

// stampAssets points the page at the stamped URLs. In dev the files come off
// disk and change while the server runs, so there the stamp changes per load.
func stampAssets(body string, dev bool) string {
	v := assetStamp()
	if dev {
		v = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	for _, path := range []string{"/assets/app.css", "/assets/app.js", "/vendor/fonts.css"} {
		body = strings.ReplaceAll(body, `"`+path+`"`, `"`+path+"?v="+v+`"`)
	}
	// app.js imports the renderer for the version it is looking at, and that
	// import happens in the browser rather than here, so the stamp has to reach
	// it as a value rather than as a rewritten URL.
	return strings.ReplaceAll(body, "__CW_STAMP__", v)
}

// cacheAssets is what the stamp buys. Without one the answer is no-cache,
// because a URL that does not change must not be kept.
func cacheAssets(next http.Handler, dev bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dev || r.URL.Query().Get("v") == "" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		}
		// no-cache means revalidate, not do not cache, and revalidating needs
		// something to revalidate against. An embedded file has no modification
		// time, so without this the answer to every conditional request is the
		// whole file again. The stamp is already the identity of this build.
		if !dev {
			w.Header().Set("Etag", `"`+assetStamp()+`"`)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) assets() http.Handler {
	if s.dev {
		return http.FileServer(http.Dir("web"))
	}
	return http.FileServer(http.FS(s.web))
}

// routes is the whole local surface in one place, so a test drives the
// server through the same mux a browser does rather than a handler picked
// out by hand. The hosted server has its own, and the two overlap only in
// what a walkthrough is read against: the schema and the assets.
func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/assets/", cacheAssets(http.StripPrefix("/assets/", s.assets()), s.dev))
	mux.HandleFunc(vendorPrefix, s.vendor.Handler())
	mux.HandleFunc("/schema.json", schemaHandler(FormatDefault))
	mux.HandleFunc("/schema/v1.json", schemaHandler(FormatV1))
	mux.HandleFunc("/schema/v2.json", schemaHandler(FormatV2))
	mux.HandleFunc("/api/walkthrough", s.guard(s.handleWalkthrough))
	mux.HandleFunc("/api/state", s.guard(s.handleState))
	mux.HandleFunc("/api/open", s.guard(s.handleOpen))
	mux.HandleFunc("/api/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/api/ides", s.guard(s.handleIDEs))
	mux.HandleFunc("/api/comments", s.guard(s.handleComments))
	mux.HandleFunc("/api/comments/", s.guard(s.handleComment))
	return mux
}

// guard keeps another page in the browser from driving this server. Everything
// under /api needs the token the page was served with, and a cross-origin
// caller is turned away before anything is read or opened.
func (s *server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && !strings.HasPrefix(o, "http://127.0.0.1:") && !strings.HasPrefix(o, "http://localhost:") {
			http.Error(w, "cross-origin requests are not served", http.StatusForbidden)
			return
		}
		if r.Header.Get("X-Cw-Token") != s.token {
			http.Error(w, "missing or stale token, reload the page", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	var raw []byte
	var err error
	if s.dev {
		raw, err = os.ReadFile(filepath.Join("web", "index.html"))
	} else {
		raw, err = fs.ReadFile(s.web, "index.html")
	}
	if err != nil {
		http.Error(w, "index.html is missing from this build", http.StatusInternalServerError)
		return
	}
	body := stampAssets(strings.ReplaceAll(string(raw), "__CW_TOKEN__", s.token), s.dev)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// schemaHandler serves one version of the contract. /schema.json is whichever
// version is current, which is what an editor points at when it wants to follow
// along; the numbered URLs are what a document pins itself to and they never
// move. Both servers answer the same three, because a walkthrough read locally
// and the same one read on the site are the same file.
func schemaHandler(version string) http.HandlerFunc {
	body := schemaBytes(version)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(body)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) build() (*payload, error) {
	res, err := LoadDoc(s.file)
	if err != nil {
		return nil, err
	}
	view := res.View()
	settings, spath, _ := LoadSettings()
	tree := &Tree{Root: s.root}
	_, moved, stale := tree.Verify(view)

	// The bytes are taken after Verify and not before, because Verify writes
	// what it found into the document. The page shows what this machine sees
	// now, not what the publisher saw on theirs.
	raw, err := view.Raw()
	if err != nil {
		return nil, err
	}
	comments := s.commentsView()
	return &payload{
		Doc: raw, Root: filepath.ToSlash(s.root), RootName: rootName(s.root),
		File: filepath.ToSlash(s.file), Settings: settings, SetPath: spath, IDEs: DetectIDEs(),
		Errors: res.Errors, Warnings: res.Warnings, Moved: moved, Stale: stale, Stamp: s.stamp(view),
		Comments: &comments,
	}, nil
}

// stamp changes whenever the walkthrough or a file it points at is touched, so
// the page can offer a reload instead of quietly showing yesterday.
func (s *server) stamp(w *Walkthrough) string {
	var latest int64
	if st, err := os.Stat(s.file); err == nil {
		latest = st.ModTime().UnixNano()
	}
	n := 0
	tree := &Tree{Root: s.root}
	if s.root != "" {
		for _, f := range w.Files {
			abs, _, err := tree.safePath(f)
			if err != nil {
				continue
			}
			if st, err := os.Stat(abs); err == nil {
				n++
				if m := st.ModTime().UnixNano(); m > latest {
					latest = m
				}
			}
		}
	}
	return fmt.Sprintf("%d-%d-%d", latest, n, w.Steps)
}

func (s *server) handleWalkthrough(w http.ResponseWriter, r *http.Request) {
	p, err := s.build()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"fatal": err.Error(), "file": filepath.ToSlash(s.file)})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	res, err := LoadDoc(s.file)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"stamp": "unreadable", "fatal": err.Error()})
		return
	}
	rev, watching := s.comments.state()
	writeJSON(w, http.StatusOK, map[string]any{"stamp": s.stamp(res.View()),
		"comments": map[string]any{"rev": rev, "watching": watching}})
}

func (s *server) handleIDEs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ides": DetectIDEs(), "auto": firstAvailableIDE()})
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cur, path, _ := LoadSettings()
		writeJSON(w, http.StatusOK, map[string]any{"settings": cur, "path": path})
	case http.MethodPut:
		var in Settings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		saved, err := SaveSettings(in)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		path, _ := settingsPath()
		writeJSON(w, http.StatusOK, map[string]any{"settings": saved, "path": path})
	default:
		http.Error(w, "use GET or PUT", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.root == "" {
		writeJSON(w, http.StatusBadRequest, OpenResult{
			Message: "this walkthrough has no root, so there is no file to open. Give one with --root or in the file itself"})
		return
	}
	tree := &Tree{Root: s.root}
	abs, rel, err := tree.safePath(in.File)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, OpenResult{Message: err.Error()})
		return
	}
	if _, err := os.Stat(abs); err != nil {
		writeJSON(w, http.StatusNotFound, OpenResult{Message: rel + " is not in the working tree"})
		return
	}
	settings, _, _ := LoadSettings()
	out := OpenInIDE(settings, abs, in.Line, in.Col)
	code := http.StatusOK
	if !out.OK {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, out)
}
