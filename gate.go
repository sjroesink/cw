package main

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

/*
The lock on a walkthrough, and what it takes to get past it.

Two independent conditions, and a walkthrough that sets both needs both.
Being on the office network is not the same claim as knowing the password, and
letting either stand in for the other would make the pair weaker than the
stronger half of it.

Holding the key that can change a walkthrough gets past both, because someone
who can rewrite it can obviously read it, and because it is what lets the person
who published it open their own page without typing their own password.
*/

type gate struct {
	// Why the reader was turned away, for a page that has to say something
	// useful rather than 401.
	NeedsPassword bool
	BlockedByIP   bool
	IP            net.IP
}

func (g gate) open() bool { return !g.NeedsPassword && !g.BlockedByIP }

// mayRead works out what, if anything, still stands between this request and
// the walkthrough.
func (h *hostServer) mayRead(r *http.Request, slug string) gate {
	ip := clientIP(r, h.trusted)

	// The key that can change it also opens it. Nothing else about the request
	// matters in that case.
	if key := bearer(r); key != "" && h.store.MayEdit(slug, key) {
		return gate{IP: ip}
	}

	g := gate{IP: ip}
	if !h.store.AllowsIP(slug, ip) {
		g.BlockedByIP = true
	}
	if h.store.HasPassword(slug) && !h.knowsPassword(r, slug) {
		g.NeedsPassword = true
	}
	return g
}

// knowsPassword covers the two ways of showing that you have it. A browser
// unlocks once and carries a cookie; something without a cookie jar sends the
// password on the request, which is what makes cw open work on a locked page.
func (h *hostServer) knowsPassword(r *http.Request, slug string) bool {
	if hasUnlockCookie(r, h.secret, slug) {
		return true
	}
	if pw := r.Header.Get("X-Cw-Password"); pw != "" {
		return h.store.CheckPassword(slug, pw)
	}
	return false
}

// refuse answers a reader who cannot see this walkthrough. An address that is
// not allowed is a flat no: telling someone their address is wrong is fine,
// telling them a password would help is an invitation to go and find one.
func (h *hostServer) refuse(w http.ResponseWriter, slug string, g gate) {
	if g.BlockedByIP {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"message": "this walkthrough is limited to certain addresses, and " + g.IP.String() + " is not one of them",
			"ip":      g.IP.String(),
		})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"message":  "this walkthrough needs a password",
		"locked":   true,
		"unlockAt": "/api/v1/walkthroughs/" + slug + "/unlock",
	})
}

// handleUnlock takes a password and, if it is the right one, hands back a
// cookie that stands for having typed it. The cookie is a signature over this
// one name, so it does not open anything else.
func (h *hostServer) handleUnlock(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !ValidSlug(slug) || !h.store.Exists(slug) {
		h.fail(w, http.StatusNotFound, "no walkthrough called %q", slug)
		return
	}
	// An address that is not allowed cannot get in by knowing the password.
	if !h.store.AllowsIP(slug, clientIP(r, h.trusted)) {
		h.refuse(w, slug, gate{BlockedByIP: true, IP: clientIP(r, h.trusted)})
		return
	}
	if !h.store.HasPassword(slug) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "this walkthrough has no password"})
		return
	}

	var in struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		h.fail(w, http.StatusBadRequest, "send {\"password\": \"...\"}")
		return
	}
	if !h.store.CheckPassword(slug, in.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "that is not the password"})
		return
	}
	setUnlockCookie(w, h.secret, slug, strings.HasPrefix(h.base, "https://"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleWhoami says which address the site believes you are calling from. It
// exists so that nobody has to guess at what to put in an address list, and so
// that a rule that turns out to block the person who wrote it can be diagnosed
// in one request instead of by trial and error.
func (h *hostServer) handleWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ip":      clientIP(r, h.trusted).String(),
		"socket":  hostOf(r.RemoteAddr).String(),
		"trusted": h.trustedFrom,
	})
}

// visible filters a listing down to what this reader is allowed to see. A
// walkthrough somebody locked should not have its title read out on the front
// page to everyone who did not unlock it.
func (h *hostServer) visible(r *http.Request, all []Meta) []Meta {
	if _, isAdmin := h.store.MatchKey(bearer(r)); isAdmin {
		return all
	}
	out := make([]Meta, 0, len(all))
	for _, m := range all {
		if !m.Locked || h.mayRead(r, m.Slug).open() {
			out = append(out, m)
		}
	}
	return out
}
