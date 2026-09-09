package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

/*
Who is asking, and may they read this.

Working out the caller's address is the whole difficulty. Here the chain is
Cloudflare, then Traefik, then the container, so the socket address is always
Traefik and the real visitor is somewhere in a header. A header is whatever the
sender wrote, so it counts only when every hop that could have written it is one
we trust: the walk goes right to left through X-Forwarded-For, skipping trusted
hops, and stops at the first address that was not put there by our own
infrastructure. That address is the visitor.

Trust nothing and the answer is the socket address, which behind a proxy is the
proxy. That is the safe failure: an IP rule that lets nobody in gets reported,
an IP rule that lets everybody in does not.
*/

// cloudflareRanges is the published edge list, taken on 2026-09-09 from
// cloudflare.com/ips-v4 and ips-v6. It changes a few times a year, so a build
// that has been running for a long time is worth re-checking; CW_TRUSTED_PROXIES
// overrides all of this when it is set.
var cloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

// privateRanges are the hops that can only be our own: the container network,
// the host, and the LAN Traefik sits on. The container is not reachable from
// the internet, so anything arriving from one of these came through it.
var privateRanges = []string{
	"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
}

type netList struct {
	nets []*net.IPNet
	raw  []string
}

func parseNets(entries []string) (*netList, error) {
	out := &netList{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		// A bare address is the /32 or /128 it stands for, because writing the
		// mask for one machine is the kind of thing people get wrong.
		if !strings.Contains(e, "/") {
			if ip := net.ParseIP(e); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				e = fmt.Sprintf("%s/%d", ip, bits)
			}
		}
		_, n, err := net.ParseCIDR(e)
		if err != nil {
			return nil, fmt.Errorf("%q is not an address or a range: %w", e, err)
		}
		out.nets = append(out.nets, n)
		out.raw = append(out.raw, e)
	}
	return out, nil
}

func (l *netList) has(ip net.IP) bool {
	if l == nil || ip == nil {
		return false
	}
	for _, n := range l.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (l *netList) empty() bool { return l == nil || len(l.nets) == 0 }

// trustedProxies is what may be believed about who the caller is.
// CW_TRUSTED_PROXIES replaces the default entirely, so a deployment that is not
// behind Cloudflare does not silently keep trusting it.
func trustedProxies() (*netList, string, error) {
	if raw := strings.TrimSpace(os.Getenv("CW_TRUSTED_PROXIES")); raw != "" {
		list, err := parseNets(strings.Split(raw, ","))
		return list, "CW_TRUSTED_PROXIES", err
	}
	list, err := parseNets(append(append([]string{}, privateRanges...), cloudflareRanges...))
	return list, "the private ranges and Cloudflare's published edges", err
}

// clientIP walks the chain from the socket outwards and stops at the first hop
// nobody we trust could have forged.
func clientIP(r *http.Request, trusted *netList) net.IP {
	peer := hostOf(r.RemoteAddr)
	if peer == nil || !trusted.has(peer) {
		return peer
	}

	// Cloudflare states the visitor plainly, and it is only worth reading when
	// the hop that reached us is one of theirs or one of ours.
	if cf := hostOf(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); cf != nil {
		return cf
	}

	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		ip := hostOf(strings.TrimSpace(chain[i]))
		if ip == nil {
			continue
		}
		if !trusted.has(ip) {
			return ip
		}
	}
	return peer
}

// hostOf takes an address with or without a port and gives back the address.
func hostOf(addr string) net.IP {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return net.ParseIP(host)
	}
	return nil
}

// ---------------------------------------------------------------- unlocking

// unlockToken is what the cookie carries after somebody has typed the password:
// a signature over the name of the walkthrough, so it proves one unlock and
// cannot be moved to another. There is nothing in it worth stealing that the
// password did not already give away.
func unlockToken(secret []byte, slug string) string {
	return hmacHex(secret, "unlock:"+slug)
}

func unlockCookieName(slug string) string { return "cw_unlock_" + slug }

func setUnlockCookie(w http.ResponseWriter, secret []byte, slug string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookieName(slug),
		Value:    unlockToken(secret, slug),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
}

func hasUnlockCookie(r *http.Request, secret []byte, slug string) bool {
	c, err := r.Cookie(unlockCookieName(slug))
	if err != nil {
		return false
	}
	return constantEqual(c.Value, unlockToken(secret, slug))
}
