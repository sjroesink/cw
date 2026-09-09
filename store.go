package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	<data>/walkthroughs/<slug>/meta.json   who published it, when, and what was verified
	<data>/keys.json                       API keys, as hashes

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
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	Number    string    `json:"number,omitempty"`
	URL       string    `json:"url,omitempty"`
	Publisher string    `json:"publisher,omitempty"`
	Steps     int       `json:"steps"`
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

func (s *Store) Put(slug string, d *Doc, m Meta) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("%q cannot be a walkthrough name: lowercase letters, digits and dashes, up to 64 characters", slug)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dirFor(slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dir, "doc.json"), d); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(dir, "meta.json"), m)
}

func (s *Store) Get(slug string) (*Doc, *Meta, error) {
	if !ValidSlug(slug) {
		return nil, nil, ErrNoSuchWalkthrough
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.dirFor(slug)
	var d Doc
	if err := readJSONFile(filepath.Join(dir, "doc.json"), &d); err != nil {
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
	return &d, &m, nil
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

// AddKey mints a key, stores its hash and hands back the only copy of the key
// itself. Calling it twice with the same name adds a second key, because
// rotating one is exactly that: add, move over, remove.
func (s *Store) AddKey(name string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := "cw_" + hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readKeys()
	if err != nil {
		return "", err
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

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- files

// writeJSONFile writes beside the target and renames over it, so a reader never
// catches a half-written file and a failed write leaves the old one standing.
func writeJSONFile(path string, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
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
