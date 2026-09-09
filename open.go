package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/*
Reading a published walkthrough with the buttons that only work locally.

The hosted page cannot open a file in your editor and cannot check the snippets
against your checkout, because it has neither. Rather than teaching the browser
to reach into your machine, this pulls the walkthrough the other way: fetch what
was published, serve it on 127.0.0.1, and you get the local page over hosted
content. Which is the whole local reader, unchanged.
*/

func cmdOpen(args []string) {
	site := strings.TrimRight(envOr("CW_SITE", defaultSite), "/")
	f := flags{port: -1}
	target := ""

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
		case a == "--site":
			site = strings.TrimRight(next(), "/")
		case strings.HasPrefix(a, "--site="):
			site = strings.TrimRight(strings.TrimPrefix(a, "--site="), "/")
		case a == "--port":
			f.port = atoiOr(next(), -1)
		case strings.HasPrefix(a, "--port="):
			f.port = atoiOr(strings.TrimPrefix(a, "--port="), -1)
		case a == "--no-open":
			f.noOpen = true
		case a == "--offline":
			f.offline = true
		case strings.HasPrefix(a, "-"):
			die("unknown flag %q", a)
		default:
			if target != "" {
				die("give one walkthrough, got %q and %q", target, a)
			}
			target = a
		}
	}
	if target == "" {
		die("give a walkthrough URL or its name")
	}

	url := apiURL(target, site)
	doc, name, err := fetchDoc(url)
	if err != nil {
		die("%v", err)
	}

	// The file is a copy of what is published, so it goes somewhere temporary
	// rather than into whatever directory this was run from.
	dir, err := os.MkdirTemp("", "cw-open-")
	if err != nil {
		die("%v", err)
	}
	// Whatever root the publisher had is meaningless here, and letting it
	// resolve against a temp directory would point the open buttons at
	// somewhere arbitrary. Only --root decides where the code is on this
	// machine.
	doc.Root = ""
	path := filepath.Join(dir, name+".json")
	if err := WriteDoc(path, doc); err != nil {
		die("%v", err)
	}

	fmt.Printf("%s\n  fetched from %s\n\n", doc.Title, url)
	f.file = path
	runServe(f)
}

// apiURL takes whatever the reader pasted and works out what to fetch: a page
// URL, an API URL, or a bare name.
func apiURL(target, site string) string {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return site + "/api/v1/walkthroughs/" + target
	}
	target = strings.TrimRight(target, "/")
	if i := strings.Index(target, "/w/"); i >= 0 {
		return target[:i] + "/api/v1/walkthroughs/" + target[i+len("/w/"):]
	}
	return target
}

func fetchDoc(url string) (*Doc, string, error) {
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s answered %s", url, resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", err
	}
	var body struct {
		Doc  *Doc  `json:"doc"`
		Meta *Meta `json:"meta"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Doc == nil {
		return nil, "", fmt.Errorf("%s did not answer with a walkthrough", url)
	}
	name := "walkthrough"
	if body.Meta != nil && body.Meta.Slug != "" {
		name = body.Meta.Slug
	}
	return body.Doc, name, nil
}
