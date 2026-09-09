package main

import (
	"crypto/rand"
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
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed all:web
var webFS embed.FS

//go:embed schema/walkthrough.schema.json
var schemaJSON []byte

const usageText = `cw: serve a code walkthrough as a page you can step through.

  cw serve <walkthrough.json> [flags]   open it in a browser
  cw check <walkthrough.json> [flags]   validate against the schema, no server
  cw migrate <walkthrough.json>         lift an older file to cw/1 and fill in ids and anchors
  cw schema [--write]                   print the JSON schema, or write a copy to point at
  cw ides                               list the editors found on this machine
  cw settings [--path]                  print the settings file
  cw cache warm | clear                 fetch the mermaid and typeface bundle, or drop it

Sharing one:
  cw publish <walkthrough.json> [--site URL] [--slug NAME] [--new] [--force]
  cw open <url or name> [--root DIR]    read a published one with the local buttons

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
	os.Exit(2)
}

func mustSchema() *schemaDoc {
	sch, err := loadSchema(schemaJSON)
	if err != nil {
		die("%v", err)
	}
	return sch
}

// resolveRoot decides which checkout the paths hang off: what was asked for,
// what the walkthrough says, or the repository the walkthrough sits in. An
// empty root is allowed: without one the page still reads, it just cannot open
// anything or say whether the code is still there.
func resolveRoot(f flags, d *Doc) string {
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
	if d != nil {
		if r := pick(d.Root, dir); r != "" {
			return r
		}
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
	res, err := LoadDoc(f.file, mustSchema())
	if err != nil {
		die("%v", err)
	}
	root := resolveRoot(f, res.Doc)
	tree := &Tree{Root: root}
	checked, moved, stale := tree.Verify(res.Doc)

	fmt.Printf("%s\n", res.Doc.Title)
	fmt.Printf("  parts %d, steps %d\n", len(res.Doc.Parts), res.Doc.Steps())
	if root == "" {
		fmt.Printf("  root  none, so nothing was checked against a working tree\n")
	} else {
		fmt.Printf("  root  %s\n", root)
	}
	fmt.Println()

	for pi, p := range res.Doc.Parts {
		fmt.Printf("  [%d] %s\n", pi+1, p.Title)
		for _, s := range p.Sections {
			fmt.Printf("      %s\n", s.Title)
			for _, st := range s.Steps {
				if st.Code != nil {
					report("        ", st.Code.File, st.Code.Check)
				}
				if st.Diagram != nil {
					for key, r := range st.Diagram.Refs {
						report("        ", r.File+" ("+key+")", r.Check)
					}
				}
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

func report(indent, what string, c *Check) {
	if c == nil {
		return
	}
	switch c.State {
	case "ok":
		fmt.Printf("%sok     %s\n", indent, what)
	case "unchecked":
		fmt.Printf("%s-      %s\n", indent, what)
	case "moved":
		fmt.Printf("%sMOVED  %s, %s\n", indent, what, c.Note)
	default:
		note := c.Note
		if note == "" {
			note = c.State
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
	f := parseFlags(args, true)
	res, err := LoadDoc(f.file, mustSchema())
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
	ids := EnsureIDs(res.Doc)
	anchors := EnsureAnchors(res.Doc)
	if err := WriteDoc(f.file, res.Doc); err != nil {
		die("%v", err)
	}
	fmt.Printf("%s is now %s\n", f.file, FormatVersion)
	fmt.Printf("  %d id(s) filled in, %d snippet anchor(s) filled in\n", ids, anchors)
}

// WriteDoc writes a walkthrough back over itself, indented the way a hand-edited
// file is and with the schema reference kept at the top.
func WriteDoc(path string, d *Doc) error {
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

// ---------------------------------------------------------------- small commands

// The schema lives in the binary, so the honest answer to "where is it" is a
// copy on disk the author can point their editor at.
func cmdSchema(args []string) {
	if len(args) == 0 {
		fmt.Print(string(schemaJSON))
		return
	}
	if args[0] != "--write" {
		die("the only schema flag is --write")
	}
	sp, err := settingsPath()
	if err != nil {
		die("%v", err)
	}
	out := filepath.Join(filepath.Dir(sp), "walkthrough.schema.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		die("%v", err)
	}
	if err := os.WriteFile(out, schemaJSON, 0o644); err != nil {
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
	schema *schemaDoc
}

type payload struct {
	Doc      *Doc     `json:"doc"`
	Root     string   `json:"root"`
	RootName string   `json:"rootName"`
	File     string   `json:"file"`
	Settings Settings `json:"settings"`
	SetPath  string   `json:"settingsPath"`
	IDEs     []IDE    `json:"ides"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Moved    int      `json:"moved"`
	Stale    int      `json:"stale"`
	Stamp    string   `json:"stamp"`

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

	sch := mustSchema()
	res, err := LoadDoc(abs, sch)
	if err != nil {
		die("%v", err)
	}
	root := resolveRoot(f, res.Doc)

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		die("the web assets are missing from this build: %v", err)
	}
	s := &server{file: abs, root: root, dev: f.dev, token: randomToken(),
		vendor: NewVendor(settings.Offline), web: sub, schema: sch}
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

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/assets/", http.StripPrefix("/assets/", s.assets()))
	mux.HandleFunc(vendorPrefix, s.vendor.Handler())
	mux.HandleFunc("/schema.json", s.handleSchema)
	mux.HandleFunc("/api/walkthrough", s.guard(s.handleWalkthrough))
	mux.HandleFunc("/api/state", s.guard(s.handleState))
	mux.HandleFunc("/api/open", s.guard(s.handleOpen))
	mux.HandleFunc("/api/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/api/ides", s.guard(s.handleIDEs))

	fmt.Printf("%s\n", res.Doc.Title)
	fmt.Printf("  url       %s\n", url)
	if root == "" {
		fmt.Printf("  root      none: the open buttons will say so\n")
	} else {
		fmt.Printf("  root      %s\n", root)
	}
	fmt.Printf("  editor    %s\n", describeIDE(settings))
	fmt.Printf("  settings  %s\n", spath)
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

func (s *server) assets() http.Handler {
	if s.dev {
		return http.FileServer(http.Dir("web"))
	}
	return http.FileServer(http.FS(s.web))
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
	body := strings.ReplaceAll(string(raw), "__CW_TOKEN__", s.token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

func (s *server) handleSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(schemaJSON)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) build() (*payload, error) {
	res, err := LoadDoc(s.file, s.schema)
	if err != nil {
		return nil, err
	}
	settings, spath, _ := LoadSettings()
	tree := &Tree{Root: s.root}
	_, moved, stale := tree.Verify(res.Doc)

	return &payload{
		Doc: res.Doc, Root: filepath.ToSlash(s.root), RootName: rootName(s.root),
		File: filepath.ToSlash(s.file), Settings: settings, SetPath: spath, IDEs: DetectIDEs(),
		Errors: res.Errors, Warnings: res.Warnings, Moved: moved, Stale: stale, Stamp: s.stamp(res.Doc),
	}, nil
}

// stamp changes whenever the walkthrough or a file it points at is touched, so
// the page can offer a reload instead of quietly showing yesterday.
func (s *server) stamp(d *Doc) string {
	var latest int64
	if st, err := os.Stat(s.file); err == nil {
		latest = st.ModTime().UnixNano()
	}
	n := 0
	tree := &Tree{Root: s.root}
	if s.root != "" {
		for _, f := range d.Files() {
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
	return fmt.Sprintf("%d-%d-%d", latest, n, d.Steps())
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
	res, err := LoadDoc(s.file, s.schema)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"stamp": "unreadable", "fatal": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stamp": s.stamp(res.Doc)})
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
