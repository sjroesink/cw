package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
Where a published walkthrough lives. A directory, not a database: the binary
stays dependency-free, the volume can be tarred up and read back by anyone, and
a walkthrough on disk is the same JSON that was posted. If this ever needs a
real database it will be because there are enough of them to page through, and
that is a good problem to have first.

	<data>/walkthroughs/<slug>/doc.json    the walkthrough itself, cw/1
	<data>/walkthroughs/<slug>/meta.json   when it was published and what was verified
	<data>/walkthroughs/<slug>/key         the hash of the key that may change it
	<data>/walkthroughs/<slug>/password    the hash of the password to read it, if it has one
	<data>/secret                          what unlock cookies are signed with
	<data>/keys.json                       admin keys, as hashes

The edit key lives in its own file rather than in meta.json, because meta.json
is handed to anyone who opens the page. A secret that is not in the struct that
gets serialised cannot be leaked by adding a field to a response later.

Every write goes to a temp file and is then renamed, so a reader either sees the
version before or the version after and never half of one.
*/

var ErrNoSuchWalkthrough = errors.New("no walkthrough with that name")

// slugPattern is close to an id in the format, and stricter at the ends: a slug
// is a directory name and the tail of a URL, so it starts and ends on something
// other than a dash. It is built from user input, so it is checked rather than
// cleaned: a name that does not fit is refused with the rule, not silently
// turned into a different one.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

type Store struct {
	Dir string
	mu  sync.RWMutex
}

// Meta is everything about a published walkthrough that is not the walkthrough.
// It is kept beside the document rather than inside it, so the document stays a
// portable file that says nothing about where it happens to be hosted.
type Meta struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Number    string `json:"number,omitempty"`
	URL       string `json:"url,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Steps     int    `json:"steps"`
	// Format is which version of the walkthrough format doc.json is written in.
	// Nothing on this server reads it. It goes out in the listing, where the
	// thing that cares is an agent deciding whether a walkthrough is worth
	// migrating, and a store holding two formats and recording neither can only
	// answer that by opening every document. Absent means it was published
	// before the field existed, not that the format is unknown.
	Format string `json:"format,omitempty"`
	// Locked says the walkthrough asks something of a reader before it opens.
	// It is a fact about the page, not a secret, and it is what keeps a
	// protected walkthrough out of a listing somebody else is reading.
	Locked    bool      `json:"locked,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Verified  *Verified `json:"verified,omitempty"`
}

// Verified is what the publisher's working tree said about the snippets at the
// moment they were published. The hosted page has no tree of its own, so this
// is the only honest thing it can say about whether the code is still there.
type Verified struct {
	Commit  string    `json:"commit,omitempty"`
	At      time.Time `json:"at"`
	Checked int       `json:"checked"`
	Moved   int       `json:"moved"`
	Stale   int       `json:"stale"`
}

func NewStore(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "walkthroughs"), 0o755); err != nil {
		return nil, fmt.Errorf("cannot use %s as the data directory: %w", abs, err)
	}
	return &Store{Dir: abs}, nil
}

// ValidSlug says whether a name may be used as a path segment and as the tail
// of a URL. Anything else is refused with the rule rather than corrected into
// something the caller did not ask for.
func ValidSlug(slug string) bool { return slugPattern.MatchString(slug) }

func (s *Store) dirFor(slug string) string {
	return filepath.Join(s.Dir, "walkthroughs", slug)
}

func (s *Store) Exists(slug string) bool {
	if !ValidSlug(slug) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(filepath.Join(s.dirFor(slug), "doc.json"))
	return err == nil
}

// Put takes the document as bytes rather than as a struct. The store holds
// walkthroughs of more than one version and has no business re-encoding one
// through the struct of the other, which is how a field nobody thought about
// goes missing.
func (s *Store) Put(slug string, doc json.RawMessage, m Meta) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("%q cannot be a walkthrough name: lowercase letters, digits and dashes, up to 64 characters", slug)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dirFor(slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dir, "doc.json"), doc); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(dir, "meta.json"), m)
}

func (s *Store) Get(slug string) (json.RawMessage, *Meta, error) {
	if !ValidSlug(slug) {
		return nil, nil, ErrNoSuchWalkthrough
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.dirFor(slug)
	doc, err := os.ReadFile(filepath.Join(dir, "doc.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNoSuchWalkthrough
		}
		return nil, nil, err
	}
	var m Meta
	if err := readJSONFile(filepath.Join(dir, "meta.json"), &m); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	m.Slug = slug
	return doc, &m, nil
}

// List returns the metadata of everything published, newest first. A directory
// without a readable meta.json is skipped rather than failing the listing: one
// broken walkthrough should not hide the rest.
func (s *Store) List() ([]Meta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(filepath.Join(s.Dir, "walkthroughs"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var m Meta
		if err := readJSONFile(filepath.Join(s.dirFor(e.Name()), "meta.json"), &m); err != nil {
			continue
		}
		m.Slug = e.Name()
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *Store) Delete(slug string) error {
	if !ValidSlug(slug) {
		return ErrNoSuchWalkthrough
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dirFor(slug)
	if _, err := os.Stat(dir); err != nil {
		return ErrNoSuchWalkthrough
	}
	return os.RemoveAll(dir)
}

// FreeSlug turns a wanted name into one nothing else is using, by counting up.
// It is only reached when the publisher did not ask for a particular name.
func (s *Store) FreeSlug(want string) string {
	if want == "" {
		want = "walkthrough"
	}
	if !s.Exists(want) {
		return want
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", want, n)
		if !s.Exists(candidate) {
			return candidate
		}
	}
}

// ---------------------------------------------------------------- keys

// Key is an API key as it is kept: the hash, never the key. A lost key is
// replaced rather than recovered, which is the whole point of storing it this
// way.
type Key struct {
	Name      string    `json:"name"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
	LastUsed  time.Time `json:"lastUsed,omitempty"`
}

type keyFile struct {
	Keys []Key `json:"keys"`
}

func (s *Store) keysPath() string { return filepath.Join(s.Dir, "keys.json") }

func (s *Store) Keys() ([]Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readKeys()
}

func (s *Store) readKeys() ([]Key, error) {
	var kf keyFile
	if err := readJSONFile(s.keysPath(), &kf); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return kf.Keys, nil
}

// ---------------------------------------------------------------- edit keys

// mintKey makes a key and hands back the only copy. Callers store the hash.
func mintKey(prefix string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

func (s *Store) keyFileFor(slug string) string {
	return filepath.Join(s.dirFor(slug), "key")
}

// SetEditKey mints the key that may change this walkthrough and returns it. It
// is the only time the key exists anywhere but in the hands of whoever
// published: only its hash is written down.
func (s *Store) SetEditKey(slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", ErrNoSuchWalkthrough
	}
	key, err := mintKey("cwp_")
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dirFor(slug), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.keyFileFor(slug), []byte(hashKey(key)), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// MayEdit says whether the presented key is allowed to change this walkthrough:
// its own key, or an admin key, which is what makes a lost key recoverable and
// a mistake removable. Both comparisons run in constant time, and an unclaimed
// walkthrough is admin-only rather than open to anyone.
func (s *Store) MayEdit(slug, presented string) bool {
	if presented == "" {
		return false
	}
	s.mu.RLock()
	want, err := os.ReadFile(s.keyFileFor(slug))
	s.mu.RUnlock()
	if err == nil && len(want) > 0 {
		if subtle.ConstantTimeCompare(want, []byte(hashKey(presented))) == 1 {
			return true
		}
	}
	_, isAdmin := s.MatchKey(presented)
	return isAdmin
}

// ---------------------------------------------------------------- admin keys

// AddKey mints a key, stores its hash and hands back the only copy of the key
// itself. Calling it twice with the same name adds a second key, because
// rotating one is exactly that: add, move over, remove.
func (s *Store) AddKey(name string) (string, error) {
	key, err := mintKey("cw_")
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err2 := s.readKeys()
	if err2 != nil {
		return "", err2
	}
	keys = append(keys, Key{Name: name, Hash: hashKey(key), CreatedAt: time.Now().UTC()})
	if err := writeJSONFile(s.keysPath(), keyFile{Keys: keys}); err != nil {
		return "", err
	}
	return key, nil
}

// AddKeyValue stores a key someone else chose, for the bootstrap case where the
// key has to be known before the container starts.
func (s *Store) AddKeyValue(name, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readKeys()
	if err != nil {
		return err
	}
	want := hashKey(key)
	for _, k := range keys {
		if k.Hash == want {
			return nil
		}
	}
	keys = append(keys, Key{Name: name, Hash: want, CreatedAt: time.Now().UTC()})
	return writeJSONFile(s.keysPath(), keyFile{Keys: keys})
}

func (s *Store) RemoveKey(name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readKeys()
	if err != nil {
		return 0, err
	}
	kept := keys[:0]
	removed := 0
	for _, k := range keys {
		if k.Name == name {
			removed++
			continue
		}
		kept = append(kept, k)
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, writeJSONFile(s.keysPath(), keyFile{Keys: kept})
}

// MatchKey compares in constant time against every key, and keeps comparing
// after it has found one, so that how long the answer takes says nothing about
// which key was presented or how many exist.
func (s *Store) MatchKey(presented string) (string, bool) {
	if presented == "" {
		return "", false
	}
	s.mu.RLock()
	keys, err := s.readKeys()
	s.mu.RUnlock()
	if err != nil {
		return "", false
	}
	want := []byte(hashKey(presented))
	name, found := "", false
	for _, k := range keys {
		if subtle.ConstantTimeCompare([]byte(k.Hash), want) == 1 {
			name, found = k.Name, true
		}
	}
	return name, found
}

// hmacHex signs a short string with the site secret. Used for unlock cookies,
// which have to be unforgeable and mean nothing on their own.
func hmacHex(secret []byte, msg string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- files

// writeJSONFile writes beside the target and renames over it, so a reader never
// catches a half-written file and a failed write leaves the old one standing.
func writeJSONFile(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	body := bytes.TrimRight(buf.Bytes(), "\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readJSONFile(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// ---------------------------------------------------------------- passwords

/*
A walkthrough can be locked with a password. It is a shared secret on a link,
not an account, so what matters is that the stored form is useless when the
volume leaks: a random salt and enough PBKDF2 rounds that guessing the password
from the hash costs more than it is worth.

Like the edit key, it lives in its own file rather than in meta.json, which is
handed to every reader.
*/

const pbkdf2Rounds = 600_000

func (s *Store) passwordPathFor(slug string) string {
	return filepath.Join(s.dirFor(slug), "password")
}

// SetPassword locks a walkthrough, or unlocks it when given an empty one.
func (s *Store) SetPassword(slug, plain string) error {
	if !ValidSlug(slug) {
		return ErrNoSuchWalkthrough
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.passwordPathFor(slug)
	if plain == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(s.dirFor(slug), 0o755); err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	sum, err := pbkdf2.Key(sha256.New, plain, salt, pbkdf2Rounds, 32)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", pbkdf2Rounds, hex.EncodeToString(salt), hex.EncodeToString(sum))
	return os.WriteFile(path, []byte(line), 0o600)
}

func (s *Store) HasPassword(slug string) bool {
	if !ValidSlug(slug) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := os.Stat(s.passwordPathFor(slug))
	return err == nil && st.Size() > 0
}

// CheckPassword answers whether this is the password, and false for a
// walkthrough that has none: a caller should ask HasPassword first, so that
// "no password set" never reads as "any password will do".
func (s *Store) CheckPassword(slug, plain string) bool {
	if !ValidSlug(slug) || plain == "" {
		return false
	}
	s.mu.RLock()
	raw, err := os.ReadFile(s.passwordPathFor(slug))
	s.mu.RUnlock()
	if err != nil {
		return false
	}
	parts := strings.Split(strings.TrimSpace(string(raw)), "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[2])
	if err != nil || rounds < 1 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[3])
	want, err2 := hex.DecodeString(parts[4])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, plain, salt, rounds, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Secret is what unlock cookies are signed with. It is made once and kept, so
// that a restart does not sign everybody out.
func (s *Store) Secret() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.Dir, "secret")
	if raw, err := os.ReadFile(path); err == nil && len(raw) >= 32 {
		return raw, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// ---------------------------------------------------------------- addresses

/*
A walkthrough can also be limited to a set of addresses. Where a password is a
secret the reader has to know, this is a fact about where they are, and the two
are independent: a walkthrough with both needs both, because being on the office
network is not the same claim as knowing the password and neither should stand
in for the other.
*/

func (s *Store) allowPathFor(slug string) string {
	return filepath.Join(s.dirFor(slug), "allow")
}

// SetAllow limits a walkthrough to these addresses and ranges. An empty list
// removes the limit. Every entry is parsed before anything is written, so a
// typo cannot lock a walkthrough away from everybody.
func (s *Store) SetAllow(slug string, entries []string) error {
	if !ValidSlug(slug) {
		return ErrNoSuchWalkthrough
	}
	var keep []string
	for _, e := range entries {
		if e = strings.TrimSpace(e); e != "" {
			keep = append(keep, e)
		}
	}
	if len(keep) > 0 {
		if _, err := parseNets(keep); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.allowPathFor(slug)
	if len(keep) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(s.dirFor(slug), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

// Allow is the list as it was written, for showing back to whoever set it.
func (s *Store) Allow(slug string) []string {
	if !ValidSlug(slug) {
		return nil
	}
	s.mu.RLock()
	raw, err := os.ReadFile(s.allowPathFor(slug))
	s.mu.RUnlock()
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// AllowsIP answers whether this address may read it. A walkthrough with no list
// allows everyone; one with a list that cannot be parsed allows nobody, because
// the alternative is a restriction that quietly is not one.
func (s *Store) AllowsIP(slug string, ip net.IP) bool {
	entries := s.Allow(slug)
	if len(entries) == 0 {
		return true
	}
	list, err := parseNets(entries)
	if err != nil {
		return false
	}
	return list.has(ip)
}
