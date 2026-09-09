package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func from(t *testing.T, h *hostServer, method, path, peer string, headers map[string]string, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.routes().ServeHTTP(rec, req)
	return rec
}

func lockedHost(t *testing.T) (*hostServer, string) {
	t.Helper()
	h, admin := testHost(t)
	trusted, where, err := trustedProxies()
	if err != nil {
		t.Fatal(err)
	}
	h.trusted, h.trustedFrom = trusted, where
	secret, err := h.store.Secret()
	if err != nil {
		t.Fatal(err)
	}
	h.secret = secret
	return h, admin
}

// The whole point of an address rule is that it cannot be talked out of. A
// caller the server has no reason to trust may claim to be anyone, and must not
// be believed.
func TestAForgedForwardedForIsIgnored(t *testing.T) {
	h, _ := lockedHost(t)
	h.trusted, _ = parseNets([]string{"10.9.9.9/32"}) // one proxy, and nothing else

	cases := []struct {
		name, peer string
		headers    map[string]string
		want       string
	}{
		{"a stranger claiming to be inside", "203.0.113.7:1234",
			map[string]string{"X-Forwarded-For": "10.0.0.5"}, "203.0.113.7"},
		{"a stranger claiming Cloudflare said so", "203.0.113.7:1234",
			map[string]string{"CF-Connecting-IP": "10.0.0.5"}, "203.0.113.7"},
		{"the real proxy, reporting a visitor", "10.9.9.9:5678",
			map[string]string{"X-Forwarded-For": "198.51.100.4"}, "198.51.100.4"},
		{"the real proxy, with a chain to walk back", "10.9.9.9:5678",
			map[string]string{"X-Forwarded-For": "198.51.100.4, 10.9.9.9"}, "198.51.100.4"},
		{"the real proxy, and Cloudflare naming the visitor", "10.9.9.9:5678",
			map[string]string{"CF-Connecting-IP": "198.51.100.4", "X-Forwarded-For": "10.9.9.9"}, "198.51.100.4"},
		{"nothing claimed at all", "203.0.113.7:1234", nil, "203.0.113.7"},
	}
	for _, c := range cases {
		rec := from(t, h, "GET", "/api/v1/whoami", c.peer, c.headers, "", "")
		if !strings.Contains(rec.Body.String(), `"ip":"`+c.want+`"`) {
			t.Errorf("%s: whoami said %s, want %s", c.name, strings.TrimSpace(rec.Body.String()), c.want)
		}
	}
}

func TestAPasswordLocksReading(t *testing.T) {
	h, admin := lockedHost(t)
	rec := from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"secret","password":"hunter2","walkthrough":`+goodDoc+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publishing with a password returned %d: %s", rec.Code, rec.Body)
	}

	for _, path := range []string{"/api/v1/walkthroughs/secret", "/w/secret", "/api/v1/walkthroughs/secret/state"} {
		if rec := from(t, h, "GET", path, "10.0.0.2:1", nil, "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without the password returned %d, want 401", path, rec.Code)
		}
	}

	// It is not in a listing either: a locked walkthrough should not read its
	// own title out to everyone who has not opened it.
	rec = from(t, h, "GET", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "", "")
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("a locked walkthrough was listed to someone who cannot read it")
	}
	if rec := from(t, h, "GET", "/api/v1/walkthroughs", "10.0.0.2:1", nil, admin, ""); !strings.Contains(rec.Body.String(), "secret") {
		t.Error("the admin key cannot see a locked walkthrough in the listing")
	}

	if rec := from(t, h, "POST", "/api/v1/walkthroughs/secret/unlock", "10.0.0.2:1", nil, "", `{"password":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("the wrong password returned %d, want 401", rec.Code)
	}

	rec = from(t, h, "POST", "/api/v1/walkthroughs/secret/unlock", "10.0.0.2:1", nil, "", `{"password":"hunter2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the right password returned %d: %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("unlocking set %d cookie(s), and HttpOnly is not set as expected", len(cookies))
	}

	rec = from(t, h, "GET", "/api/v1/walkthroughs/secret", "10.0.0.2:1",
		map[string]string{"Cookie": cookies[0].Name + "=" + cookies[0].Value}, "", "")
	if rec.Code != http.StatusOK {
		t.Errorf("reading it after unlocking returned %d", rec.Code)
	}
}

// An unlock stands for one walkthrough. Carrying it to the next one must not work.
func TestAnUnlockDoesNotOpenTheNextOne(t *testing.T) {
	h, _ := lockedHost(t)
	for _, slug := range []string{"one", "two"} {
		from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
			`{"slug":"`+slug+`","password":"same","walkthrough":`+goodDoc+`}`)
	}
	rec := from(t, h, "POST", "/api/v1/walkthroughs/one/unlock", "10.0.0.2:1", nil, "", `{"password":"same"}`)
	c := rec.Result().Cookies()[0]

	// Even with the same password: the cookie is a signature over the name.
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/two", "10.0.0.2:1",
		map[string]string{"Cookie": c.Name + "=" + c.Value}, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("one unlock opened another walkthrough: %d", rec.Code)
	}
	// And the value cannot simply be renamed onto the other one.
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/two", "10.0.0.2:1",
		map[string]string{"Cookie": "cw_unlock_two=" + c.Value}, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("a renamed unlock cookie was accepted: %d", rec.Code)
	}
}

func TestAnAddressListLocksReading(t *testing.T) {
	h, _ := lockedHost(t)
	rec := from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"office","allow":["192.168.5.0/24"],"walkthrough":`+goodDoc+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publishing with an address list returned %d: %s", rec.Code, rec.Body)
	}

	if rec := from(t, h, "GET", "/api/v1/walkthroughs/office", "10.0.0.2:1", nil, "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("reading from outside the list returned %d, want 403", rec.Code)
	}
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/office", "192.168.5.44:1", nil, "", ""); rec.Code != http.StatusOK {
		t.Errorf("reading from inside the list returned %d, want 200", rec.Code)
	}

	// A list that cannot be parsed is refused when it is set, rather than
	// becoming a restriction that quietly is not one.
	if rec := from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"broken","allow":["not-an-address"],"walkthrough":`+goodDoc+`}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unparseable address list returned %d, want 400", rec.Code)
	}
}

// Both set means both are needed. Knowing the password from the wrong place is
// not enough, and being in the right place without the password is not either.
func TestBothLocksAreNeededWhenBothAreSet(t *testing.T) {
	h, _ := lockedHost(t)
	from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"both","password":"pw","allow":["192.168.5.0/24"],"walkthrough":`+goodDoc+`}`)

	// The wrong place: the password endpoint will not even talk to you.
	if rec := from(t, h, "POST", "/api/v1/walkthroughs/both/unlock", "10.0.0.2:1", nil, "", `{"password":"pw"}`); rec.Code != http.StatusForbidden {
		t.Errorf("unlocking from a blocked address returned %d, want 403", rec.Code)
	}

	// The right place, no password.
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/both", "192.168.5.44:1", nil, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("reading from an allowed address without the password returned %d, want 401", rec.Code)
	}

	// The right place, with the password.
	rec := from(t, h, "POST", "/api/v1/walkthroughs/both/unlock", "192.168.5.44:1", nil, "", `{"password":"pw"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlocking from an allowed address returned %d", rec.Code)
	}
	c := rec.Result().Cookies()[0]
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/both", "192.168.5.44:1",
		map[string]string{"Cookie": c.Name + "=" + c.Value}, "", ""); rec.Code != http.StatusOK {
		t.Errorf("both satisfied still returned %d", rec.Code)
	}
	// And the unlock does not travel: same cookie, wrong place.
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/both", "10.0.0.2:1",
		map[string]string{"Cookie": c.Name + "=" + c.Value}, "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("an unlock carried to a blocked address returned %d, want 403", rec.Code)
	}
}

// The key that can change a walkthrough opens it, which is what lets whoever
// published it read their own page without typing their own password.
func TestTheEditKeyOpensALockedWalkthrough(t *testing.T) {
	h, admin := lockedHost(t)
	rec := from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"mine","password":"pw","allow":["192.168.5.0/24"],"walkthrough":`+goodDoc+`}`)
	var out map[string]any
	mustJSON(t, rec, &out)
	key, _ := out["key"].(string)

	if rec := from(t, h, "GET", "/api/v1/walkthroughs/mine", "10.0.0.2:1", nil, key, ""); rec.Code != http.StatusOK {
		t.Errorf("the key for this walkthrough did not open it: %d", rec.Code)
	}
	if rec := from(t, h, "GET", "/api/v1/walkthroughs/mine", "10.0.0.2:1", nil, admin, ""); rec.Code != http.StatusOK {
		t.Errorf("the admin key did not open it: %d", rec.Code)
	}
}

// Updating the content of a locked walkthrough without saying anything about the
// lock must not take the lock off.
func TestUpdatingDoesNotSilentlyUnlock(t *testing.T) {
	h, _ := lockedHost(t)
	rec := from(t, h, "POST", "/api/v1/walkthroughs", "10.0.0.2:1", nil, "",
		`{"slug":"stays","password":"pw","walkthrough":`+goodDoc+`}`)
	var out map[string]any
	mustJSON(t, rec, &out)
	key, _ := out["key"].(string)

	if rec := from(t, h, "PUT", "/api/v1/walkthroughs/stays", "10.0.0.2:1", nil, key, goodDoc); rec.Code != http.StatusOK {
		t.Fatalf("updating returned %d: %s", rec.Code, rec.Body)
	}
	if !h.store.HasPassword("stays") {
		t.Error("updating the content took the password off")
	}

	// And an empty one takes it off on purpose.
	if rec := from(t, h, "PUT", "/api/v1/walkthroughs/stays", "10.0.0.2:1", nil, key,
		`{"password":"","walkthrough":`+goodDoc+`}`); rec.Code != http.StatusOK {
		t.Fatalf("removing the password returned %d", rec.Code)
	}
	if h.store.HasPassword("stays") {
		t.Error("an empty password did not remove the lock")
	}
}

func TestPasswordHashesAreSaltedAndNotTheirInput(t *testing.T) {
	s := testStore(t)
	for _, slug := range []string{"a", "b"} {
		if err := s.Put(slug, sampleDoc(slug), Meta{}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetPassword(slug, "the same password"); err != nil {
			t.Fatal(err)
		}
	}
	one := readAll(t, s.passwordPathFor("a"))
	two := readAll(t, s.passwordPathFor("b"))
	if one == two {
		t.Error("the same password stored twice produced the same bytes, so it is not salted")
	}
	if strings.Contains(one, "the same password") {
		t.Error("the password is in the file in the clear")
	}
	if !s.CheckPassword("a", "the same password") {
		t.Error("the right password was rejected")
	}
	if s.CheckPassword("a", "another") || s.CheckPassword("a", "") {
		t.Error("a wrong password was accepted")
	}
	// A walkthrough with no password is not one that accepts any password.
	if s.CheckPassword("nothing-here", "anything") {
		t.Error("a walkthrough with no password accepted one")
	}
}
