package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

/*
The publishing API. It is deliberately small: post a walkthrough, get a URL.
Everything that decides whether a document is any good is the same LoadDoc and
inspect() that cw check runs on a laptop, so an agent posting straight at this
API gets the same complaints in the same words as someone with the tool
installed.

Errors refuse the document. Warnings come back with a successful publish,
because the difference between a walkthrough that validates and one somebody
wants to read is entirely in the warnings, and refusing on them would only teach
people to write around them.
*/

// maxBody is generous for prose and snippets and mean for anything else. A
// walkthrough that does not fit is not a walkthrough.
const maxBody = 8 << 20

type apiResult struct {
	OK       bool     `json:"ok"`
	Slug     string   `json:"slug,omitempty"`
	URL      string   `json:"url,omitempty"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Message  string   `json:"message,omitempty"`
}

// envelope is the long form of a publish request, for when the caller wants to
// choose the name. The short form is the walkthrough on its own, which is what
// makes "post the file you just wrote" the obvious thing to do.
type envelope struct {
	Slug        string          `json:"slug"`
	Walkthrough json.RawMessage `json:"walkthrough"`
}

func (h *hostServer) fail(w http.ResponseWriter, code int, format string, a ...any) {
	writeJSON(w, code, apiResult{Message: fmt.Sprintf(format, a...)})
}

// key pulls the bearer token out of the request and answers with the name it
// was stored under. Everything that writes goes through here.
func (h *hostServer) key(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", false
	}
	return h.store.MatchKey(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
}

func (h *hostServer) guarded(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := h.key(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="cw"`)
			h.fail(w, http.StatusUnauthorized, "this needs an API key: Authorization: Bearer cw_...")
			return
		}
		next(w, r, name)
	}
}

// readDoc turns a request body into a validated walkthrough. It returns the
// wanted name separately, because the name is about where the document is
// published and not about what it says.
func (h *hostServer) readDoc(r *http.Request) (*Doc, string, *LoadResult, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBody))
	if err != nil {
		return nil, "", nil, fmt.Errorf("could not read the body, or it is over %d MB: %w", maxBody>>20, err)
	}
	body, want := raw, ""

	// An envelope is recognised by the one key that only it has, so a bare
	// walkthrough is never mistaken for one.
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil {
		if _, isEnvelope := probe["walkthrough"]; isEnvelope {
			var env envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				return nil, "", nil, err
			}
			body, want = env.Walkthrough, env.Slug
		}
	}

	res, err := ParseDoc(body, "the posted walkthrough", h.schema)
	if err != nil {
		return nil, "", nil, err
	}
	return res.Doc, want, res, nil
}

// ---------------------------------------------------------------- publishing

func (h *hostServer) handleCreate(w http.ResponseWriter, r *http.Request, publisher string) {
	d, want, res, err := h.readDoc(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	if len(res.Errors) > 0 {
		writeJSON(w, http.StatusBadRequest, apiResult{Errors: res.Errors, Warnings: res.Warnings,
			Message: "the walkthrough was not stored"})
		return
	}
	if want != "" && !ValidSlug(want) {
		h.fail(w, http.StatusBadRequest, "%q cannot be a walkthrough name: lowercase letters, digits and dashes, up to 64 characters", want)
		return
	}
	slug := want
	if slug == "" {
		slug = h.store.FreeSlug(deriveSlug(d))
	} else if h.store.Exists(slug) {
		h.fail(w, http.StatusConflict, "there is already a walkthrough called %q. Use PUT to replace it, or ask for another name", slug)
		return
	}
	h.save(w, slug, d, res, publisher, time.Time{})
}

func (h *hostServer) handleReplace(w http.ResponseWriter, r *http.Request, publisher string) {
	slug := r.PathValue("slug")
	if !ValidSlug(slug) {
		h.fail(w, http.StatusNotFound, "no walkthrough called %q", slug)
		return
	}
	d, _, res, err := h.readDoc(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	if len(res.Errors) > 0 {
		writeJSON(w, http.StatusBadRequest, apiResult{Errors: res.Errors, Warnings: res.Warnings,
			Message: "the walkthrough was not stored, and what was there is untouched"})
		return
	}
	created := time.Time{}
	if _, old, err := h.store.Get(slug); err == nil {
		created = old.CreatedAt
	}
	h.save(w, slug, d, res, publisher, created)
}

// save is the last stretch both publishing paths share: fill in what a
// publisher should not have to type, work out what was verified, write it.
func (h *hostServer) save(w http.ResponseWriter, slug string, d *Doc, res *LoadResult, publisher string, created time.Time) {
	d.Version = FormatVersion
	d.Schema = strings.TrimRight(h.base, "/") + "/schema/v1.json"
	EnsureIDs(d)
	EnsureAnchors(d)

	now := time.Now().UTC()
	if created.IsZero() {
		created = now
	}
	m := Meta{
		Slug: slug, Title: d.Title, Summary: d.Summary, Steps: d.Steps(),
		Publisher: publisher, CreatedAt: created, UpdatedAt: now,
		Verified: verdictOf(d, now),
	}
	if d.Source != nil {
		m.Repo, m.Number, m.URL = d.Source.Repo, d.Source.Number, d.Source.URL
	}
	if err := h.store.Put(slug, d, m); err != nil {
		h.fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, apiResult{OK: true, Slug: slug,
		URL: strings.TrimRight(h.base, "/") + "/w/" + slug, Warnings: res.Warnings})
}

// verdictOf reads the check states the publisher baked into the document. This
// site has no working tree, so what a snippet was worth at publishing time is
// the only thing it can honestly report about it afterwards.
func verdictOf(d *Doc, at time.Time) *Verified {
	v := &Verified{At: at}
	if d.Source != nil {
		v.Commit = d.Source.Commit
	}
	tally := func(c *Check) {
		if c == nil || c.State == "" || c.State == "unchecked" {
			return
		}
		v.Checked++
		switch c.State {
		case "ok":
		case "moved":
			v.Moved++
		default:
			v.Stale++
		}
	}
	for pi := range d.Parts {
		for si := range d.Parts[pi].Sections {
			for ii := range d.Parts[pi].Sections[si].Steps {
				st := &d.Parts[pi].Sections[si].Steps[ii]
				if st.Code != nil {
					tally(st.Code.Check)
				}
				if st.Diagram == nil {
					continue
				}
				for _, ref := range st.Diagram.Refs {
					tally(ref.Check)
				}
			}
		}
	}
	if v.Checked == 0 && v.Commit == "" {
		return nil
	}
	return v
}

// deriveSlug names a walkthrough after what it is about, because a URL someone
// pastes into a channel should say something. innovadis-dev/Fincent plus
// "PR #3347" becomes fincent-pr-3347.
func deriveSlug(d *Doc) string {
	if d.Source != nil {
		repo := d.Source.Repo
		if i := strings.LastIndexByte(repo, '/'); i >= 0 {
			repo = repo[i+1:]
		}
		if part := strings.Trim(slug(repo)+"-"+slug(d.Source.Number), "-"); part != "" && part != "x" {
			return part
		}
	}
	return slug(d.Title)
}

// ---------------------------------------------------------------- reading

func (h *hostServer) handleValidate(w http.ResponseWriter, r *http.Request, _ string) {
	d, _, res, err := h.readDoc(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	code := http.StatusOK
	if len(res.Errors) > 0 {
		code = http.StatusBadRequest
	}
	writeJSON(w, code, apiResult{OK: len(res.Errors) == 0, Slug: deriveSlug(d),
		Errors: res.Errors, Warnings: res.Warnings})
}

func (h *hostServer) handleList(w http.ResponseWriter, r *http.Request, _ string) {
	list, err := h.store.List()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"walkthroughs": list})
}

func (h *hostServer) handleGet(w http.ResponseWriter, r *http.Request) {
	p, err := h.payload(r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, ErrNoSuchWalkthrough) {
			h.fail(w, http.StatusNotFound, "no walkthrough called %q", r.PathValue("slug"))
			return
		}
		h.fail(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleState is what the open page polls. It answers with the stamp alone, so
// a page left open overnight is not re-downloading the whole walkthrough to
// find out that nothing changed.
func (h *hostServer) handleState(w http.ResponseWriter, r *http.Request) {
	_, m, err := h.store.Get(r.PathValue("slug"))
	if err != nil {
		h.fail(w, http.StatusNotFound, "no walkthrough called %q", r.PathValue("slug"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stamp": m.UpdatedAt.UTC().Format(time.RFC3339Nano)})
}

func (h *hostServer) handleDelete(w http.ResponseWriter, r *http.Request, _ string) {
	slug := r.PathValue("slug")
	if err := h.store.Delete(slug); err != nil {
		h.fail(w, http.StatusNotFound, "no walkthrough called %q", slug)
		return
	}
	writeJSON(w, http.StatusOK, apiResult{OK: true, Slug: slug, Message: "removed"})
}

// payload is what the page reads. It is the same struct the local server hands
// over, so app.js has one shape to render and two ways of getting it.
func (h *hostServer) payload(slug string) (*payload, error) {
	d, m, err := h.store.Get(slug)
	if err != nil {
		return nil, err
	}
	stamp := ""
	if m != nil {
		stamp = m.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return &payload{
		Doc: d, Hosted: true, Meta: m, GitHub: BuildGitHubLinks(d.Source),
		Settings: defaultSettings(),
		Stamp:    stamp,
	}, nil
}
