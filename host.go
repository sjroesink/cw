package main

import (
	_ "embed"
	"fmt"
	"html"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

/*
The site. Same page, same schema, same validator as the local tool; what changes
is where the walkthrough comes from and what a line number can do.

Locally the server has the reader's working tree and their editor, so it checks
snippets against the tree and opens files. Hosted it has neither, so a snippet
links to GitHub instead and the freshness of the code is whatever the publisher's
tree said at the moment they published. Saying that out loud is better than
quietly showing yesterday's code as if it were today's.
*/

//go:embed web/landing.html
var landingHTML string

//go:embed web/unlock.html
var unlockHTML string

type hostServer struct {
	store *Store

	vendor *Vendor
	web    fs.FS
	base   string
	dev    bool

	// What may be believed about who is calling, and what signs an unlock.
	trusted     *netList
	trustedFrom string
	secret      []byte
}

func cmdHost(args []string) {
	addr := envOr("CW_ADDR", ":8080")
	data := envOr("CW_DATA", "./data")
	base := envOr("CW_BASE_URL", "")
	dev := false

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
		case a == "--addr":
			addr = next()
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimPrefix(a, "--addr=")
		case a == "--data":
			data = next()
		case strings.HasPrefix(a, "--data="):
			data = strings.TrimPrefix(a, "--data=")
		case a == "--base-url":
			base = next()
		case strings.HasPrefix(a, "--base-url="):
			base = strings.TrimPrefix(a, "--base-url=")
		case a == "--dev":
			dev = true
		default:
			die("unknown flag %q for host", a)
		}
	}

	store, err := NewStore(data)
	if err != nil {
		die("%v", err)
	}
	// A fresh volume has no keys, so nothing can be published to it. Rather
	// than making the first publish a shell session inside the container, the
	// key can be handed in at startup and is stored the first time it is seen.
	if boot := os.Getenv("CW_BOOTSTRAP_KEY"); boot != "" {
		if err := store.AddKeyValue("bootstrap", boot); err != nil {
			die("could not store the bootstrap key: %v", err)
		}
	}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		die("the web assets are missing from this build: %v", err)
	}
	trusted, from, err := trustedProxies()
	if err != nil {
		die("%v", err)
	}
	secret, err := store.Secret()
	if err != nil {
		die("could not read or make the site secret: %v", err)
	}

	settings, _, _ := LoadSettings()
	h := &hostServer{store: store, vendor: NewVendor(settings.Offline),
		web: sub, base: base, dev: dev, trusted: trusted, trustedFrom: from, secret: secret}
	h.vendor.Prewarm()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		die("cannot listen on %s: %v", addr, err)
	}
	if h.base == "" {
		h.base = "http://" + localAddr(ln)
	}

	keys, _ := store.Keys()
	fmt.Printf("cw host\n")
	fmt.Printf("  listening %s\n", ln.Addr())
	fmt.Printf("  base url  %s\n", h.base)
	fmt.Printf("  data      %s\n", store.Dir)
	fmt.Printf("  keys      %d\n", len(keys))
	fmt.Printf("  trusting  %s\n", h.trustedFrom)
	fmt.Printf("            GET %s/api/v1/whoami says which address a caller looks like from here\n", h.base)
	if len(keys) == 0 {
		// Publishing has needed no key since v0.2.0. An admin key is the way
		// back into a walkthrough whose own key somebody lost, so it is worth
		// having, but nothing is waiting on it.
		fmt.Printf("\nAnyone can publish. What is missing is an admin key, the one that opens\n"+
			"a walkthrough whose own key was lost. Add one with:\n  cw keys add <name> --data %s\n", store.Dir)
	}
	fmt.Printf("\nctrl-c to stop\n")

	if err := http.Serve(ln, h.routes()); err != nil {
		die("%v", err)
	}
}

func (h *hostServer) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", h.handleLanding)
	mux.HandleFunc("GET /w/{slug}", h.handlePage)
	mux.Handle("GET /assets/", cacheAssets(http.StripPrefix("/assets/", h.assets()), h.dev))
	mux.HandleFunc(vendorPrefix, h.vendor.Handler())

	mux.HandleFunc("GET /schema.json", schemaHandler(FormatDefault))
	mux.HandleFunc("GET /schema/v1.json", schemaHandler(FormatV1))
	mux.HandleFunc("GET /schema/v2.json", schemaHandler(FormatV2))
	mux.HandleFunc("GET /llms.txt", h.text(func() string { return LLMsTxt(h.base) }))
	mux.HandleFunc("GET /skill.md", h.text(func() string { return SkillDoc(h.base) }))
	mux.HandleFunc("GET /format", h.text(FormatDoc))

	// Reading and publishing are open. Changing something that is already there
	// needs the key that came back when it was published.
	mux.HandleFunc("GET /api/v1/walkthroughs", h.handleList)
	mux.HandleFunc("POST /api/v1/walkthroughs", h.handleCreate)
	mux.HandleFunc("GET /api/v1/walkthroughs/{slug}", h.handleGet)
	mux.HandleFunc("GET /api/v1/walkthroughs/{slug}/state", h.handleState)
	mux.HandleFunc("POST /api/v1/walkthroughs/{slug}/unlock", h.handleUnlock)
	mux.HandleFunc("GET /api/v1/whoami", h.handleWhoami)
	mux.HandleFunc("PUT /api/v1/walkthroughs/{slug}", h.owned(h.handleReplace))
	mux.HandleFunc("DELETE /api/v1/walkthroughs/{slug}", h.owned(h.handleDelete))
	mux.HandleFunc("POST /api/v1/validate", h.handleValidate)

	return mux
}

func (h *hostServer) assets() http.Handler {
	if h.dev {
		return http.FileServer(http.Dir("web"))
	}
	return http.FileServer(http.FS(h.web))
}

// text serves one of the documents an agent reads. They are plain text on
// purpose: something that arrives with a URL and no browser should get the
// words, not a page to scrape them out of.
func (h *hostServer) text(body func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte(body()))
	}
}

// handlePage serves the reader. The slug is stamped into the HTML the way the
// local server stamps its token, so the page knows what to fetch before it runs
// any of its own code.
func (h *hostServer) handlePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !h.store.Exists(slug) {
		http.NotFound(w, r)
		return
	}
	// A locked walkthrough gets a page that asks, rather than the reader
	// getting the real page and watching it fail to load its own content.
	if g := h.mayRead(r, slug); !g.open() {
		h.unlockPage(w, slug, g)
		return
	}
	raw, err := h.indexHTML()
	if err != nil {
		http.Error(w, "index.html is missing from this build", http.StatusInternalServerError)
		return
	}
	body := stampAssets(strings.ReplaceAll(string(raw), `window.CW = { token: "__CW_TOKEN__" };`,
		fmt.Sprintf(`window.CW = { hosted: true, slug: %q, source: "/api/v1/walkthroughs/%s" };`, slug, slug)), h.dev)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

func (h *hostServer) indexHTML() ([]byte, error) {
	if h.dev {
		return os.ReadFile(filepath.Join("web", "index.html"))
	}
	return fs.ReadFile(h.web, "index.html")
}

// handleLanding is the front door for a person. An agent gets llms.txt, which
// is linked from here because the fastest way to explain this site to someone
// is to show them the sentence they can paste at their own agent.
func (h *hostServer) handleLanding(w http.ResponseWriter, r *http.Request) {
	all, _ := h.store.List()
	list := h.visible(r, all)
	rows := &strings.Builder{}
	if len(list) == 0 {
		rows.WriteString(`<p class="empty">Nothing published yet.</p>`)
	}
	for _, m := range list {
		crumb := strings.TrimSpace(strings.Trim(m.Repo+" / "+m.Number, "/ "))
		fmt.Fprintf(rows, `<a class="row" href="/w/%s"><span class="t">%s</span><span class="c">%s</span><span class="d">%s · %s</span></a>`,
			html.EscapeString(m.Slug), html.EscapeString(m.Title), html.EscapeString(crumb),
			countOf(m.Steps, "step"), m.UpdatedAt.Format("2 Jan 2006"))
	}
	page := strings.NewReplacer(
		"__BASE__", html.EscapeString(strings.TrimRight(h.base, "/")),
		"__ROWS__", rows.String(),
	).Replace(landingHTML)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func localAddr(ln net.Listener) string {
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		host := tcp.IP.String()
		if tcp.IP.IsUnspecified() {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, strconv.Itoa(tcp.Port))
	}
	return ln.Addr().String()
}

// ---------------------------------------------------------------- keys

func cmdKeys(args []string) {
	if len(args) == 0 {
		die("cw keys add <name> | list | rm <name>   [--data DIR]")
	}
	data := envOr("CW_DATA", "./data")
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--data" && i+1 < len(args):
			i++
			data = args[i]
		case strings.HasPrefix(args[i], "--data="):
			data = strings.TrimPrefix(args[i], "--data=")
		default:
			rest = append(rest, args[i])
		}
	}
	store, err := NewStore(data)
	if err != nil {
		die("%v", err)
	}

	switch rest[0] {
	case "add":
		if len(rest) < 2 {
			die("cw keys add <name>: the name is how you will recognise it later")
		}
		key, err := store.AddKey(rest[1])
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("%s\n\nThis is the only time it is shown. Only its hash is stored.\n", key)
	case "list":
		keys, err := store.Keys()
		if err != nil {
			die("%v", err)
		}
		if len(keys) == 0 {
			fmt.Printf("no keys in %s\n", store.Dir)
			return
		}
		for _, k := range keys {
			fmt.Printf("%-24s added %s\n", k.Name, k.CreatedAt.Format(time.DateOnly))
		}
	case "rm":
		if len(rest) < 2 {
			die("cw keys rm <name>")
		}
		n, err := store.RemoveKey(rest[1])
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("removed %d key(s) called %q\n", n, rest[1])
	default:
		die("the key commands are add, list and rm")
	}
}

func countOf(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// unlockPage is what a locked walkthrough shows instead of itself: a password
// box, or a plain no for an address that is not allowed in. It says which of the
// two it is, because "it does not work" is the least useful thing a page can say.
func (h *hostServer) unlockPage(w http.ResponseWriter, slug string, g gate) {
	title, blurb, form := "Locked", "", "block"
	if g.BlockedByIP {
		title = "Not from here"
		blurb = "This walkthrough is limited to certain addresses, and " + html.EscapeString(g.IP.String()) +
			" is not one of them. Ask whoever published it to add you."
		form = "none"
	} else {
		blurb = "This walkthrough is protected. Enter the password you were given."
	}
	page := strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__BLURB__", blurb,
		"__FORM__", form,
		"__SLUG__", html.EscapeString(slug),
	).Replace(unlockHTML)

	code := http.StatusUnauthorized
	if g.BlockedByIP {
		code = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(page))
}
