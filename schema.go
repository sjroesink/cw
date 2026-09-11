package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

/*
A walkthrough is a JSON file with a schema next to it, and the schema is the
contract an author writes against: an editor validates while they type. This is
the same schema enforced at load time, so the two cannot drift apart.

It reads the subset of JSON Schema the walkthrough schemas actually use: type,
properties, required, additionalProperties, propertyNames, dependentRequired,
items, enum, const, minimum, minItems, minLength, minProperties, uniqueItems,
pattern, the applicators allOf, anyOf, oneOf, not and if/then/else, and local
$ref. Anything beyond that is a keyword nobody should reach for in these files,
and it is skipped rather than guessed at.

Patterns are the exception to that, because skipping one is invisible. JSON
Schema patterns are ECMA-262 and these are RE2, which has no lookahead, so a
pattern that will not compile makes loadSchema fail rather than quietly checking
nothing. cw/2 uses two idioms RE2 cannot take, and both are handled here once at
load time instead of guessed at per value.
*/

type schemaDoc struct {
	root map[string]any
	pats map[string]*pattern
}

// pattern is a compiled schema pattern, or a hand-written stand-in for one RE2
// cannot express.
type pattern struct {
	re    *regexp.Regexp
	match func(string) bool
}

func (p *pattern) ok(s string) bool {
	if p.re != nil {
		return p.re.MatchString(s)
	}
	return p.match(s)
}

func loadSchema(raw []byte) (*schemaDoc, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("the walkthrough schema itself is not valid JSON: %w", err)
	}
	d := &schemaDoc{root: m, pats: map[string]*pattern{}}
	if err := d.readPatterns(m); err != nil {
		return nil, err
	}
	return d, nil
}

// endOfText is how a schema written for ECMA-262 says "and nothing after this",
// because there $ also matches in front of a final newline. RE2's $ already
// means the end of the text, so the guard is dropped rather than translated.
const endOfText = `$(?![\s\S])`

// re2Source spells an ECMA-262 pattern the way RE2 spells it. Both rewrites are
// the same expression in another alphabet, not a loosening: the end-of-text
// guard is redundant here, and \uXXXX and \x{XXXX} are one character written
// two ways. Anything that survives this and still will not compile is a pattern
// this build refuses to pretend it is checking.
func re2Source(src string) string {
	src = strings.ReplaceAll(src, endOfText, "$")
	var b strings.Builder
	for i := 0; i < len(src); {
		if src[i] == '\\' && i+1 < len(src) {
			if src[i+1] == 'u' && i+6 <= len(src) && isHex(src[i+2:i+6]) {
				b.WriteString("\\x{" + src[i+2:i+6] + "}")
				i += 6
				continue
			}
			// Any other escape passes through whole, so a doubled backslash
			// cannot be mistaken for the start of one.
			b.WriteString(src[i : i+2])
			i += 2
			continue
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// pathGuard is the start of the one pattern in these schemas that RE2 cannot
// hold at all: cw/2's portable path leans on lookahead to say that no segment
// is "." or "..".
const pathGuard = `^(?!/)(?!.*(?:^|/)\.{1,2}`

func (d *schemaDoc) readPatterns(node any) error {
	switch n := node.(type) {
	case map[string]any:
		if src, ok := n["pattern"].(string); ok {
			if err := d.addPattern(src); err != nil {
				return err
			}
		}
		for _, child := range n {
			if err := d.readPatterns(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range n {
			if err := d.readPatterns(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *schemaDoc) addPattern(src string) error {
	if _, done := d.pats[src]; done {
		return nil
	}
	if re, err := regexp.Compile(re2Source(src)); err == nil {
		d.pats[src] = &pattern{re: re}
		return nil
	} else if strings.HasPrefix(src, `^(?:\./)?`) {
		d.pats[src] = &pattern{match: func(s string) bool { return portablePath(strings.TrimPrefix(s, "./")) }}
		return nil
	} else if strings.HasPrefix(src, pathGuard) {
		d.pats[src] = &pattern{match: portablePath}
		return nil
	} else {
		return fmt.Errorf("the schema has a pattern this build cannot check: %s (%v)", src, err)
	}
}

// portablePath is cw/2's path rule written out: repository-relative, forward
// slashes, no empty segment, nothing that walks up or stays put, and no
// character that stops it being a filename on somebody else's machine. It is
// easier to read here than as a regex, and keeping the regex in the schema is
// what lets every other validator check the same thing.
func portablePath(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		for _, r := range seg {
			if r == '\\' || r == ':' || r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return true
}

// Validate walks the document against the schema and returns every complaint,
// each one addressed by the path an author can find in their file.
func (s *schemaDoc) Validate(doc any) []string {
	v := &validator{doc: s}
	v.walk("", doc, s.root)
	sort.Strings(v.errs)
	return dedupe(v.errs)
}

type validator struct {
	doc  *schemaDoc
	errs []string
}

func (v *validator) fail(at, format string, a ...any) {
	where := at
	if where == "" {
		where = "the walkthrough"
	}
	v.errs = append(v.errs, where+": "+fmt.Sprintf(format, a...))
}

// probe answers whether a value fits a schema without saying so out loud. The
// applicators need that: an if that does not match is not a mistake, and the six
// branches of a oneOf the author never meant have nothing to complain about.
func (v *validator) probe(at string, value any, sch map[string]any) []string {
	sub := &validator{doc: v.doc}
	sub.walk(at, value, sch)
	return sub.errs
}

func (v *validator) resolve(sch map[string]any) map[string]any {
	for i := 0; i < 8; i++ {
		ref, ok := sch["$ref"].(string)
		if !ok {
			return sch
		}
		target := v.doc.lookup(ref)
		if target == nil {
			return map[string]any{}
		}
		sch = target
	}
	return sch
}

func (d *schemaDoc) lookup(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	cur := any(d.root)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[part]
		if !ok {
			return nil
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

func (v *validator) walk(at string, value any, sch map[string]any) {
	sch = v.resolve(sch)
	if len(sch) == 0 {
		return
	}

	if want, ok := sch["type"].(string); ok && !typeMatches(want, value) {
		v.fail(at, "should be %s, not %s", want, kindOf(value))
		return
	}

	if allowed, ok := sch["enum"].([]any); ok {
		if !containsValue(allowed, value) {
			v.fail(at, "%v is not one of: %s", value, joinValues(allowed))
		}
	}

	if want, ok := sch["const"]; ok && want != value {
		v.fail(at, "has to be %v, not %v", want, value)
	}

	switch actual := value.(type) {
	case map[string]any:
		v.object(at, actual, sch)
	case []any:
		v.array(at, actual, sch)
	case string:
		v.text(at, actual, sch)
	case float64:
		v.number(at, actual, sch)
	}

	v.applicators(at, value, sch)
}

func (v *validator) object(at string, obj map[string]any, sch map[string]any) {
	props, _ := sch["properties"].(map[string]any)

	if min, ok := numberOf(sch["minProperties"]); ok && float64(len(obj)) < min {
		if min == 1 {
			v.fail(at, "is empty, so leave it out rather than writing it as {}")
		} else {
			v.fail(at, "needs at least %d fields, and has %d", int(min), len(obj))
		}
	}

	if req, ok := sch["required"].([]any); ok {
		var missing []string
		for _, r := range req {
			name, _ := r.(string)
			if _, present := obj[name]; !present {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			v.fail(at, "is missing %s", strings.Join(quoteAll(missing), ", "))
		}
	}

	// A field that only means something next to another one. Saying which one is
	// missing is the whole value of the keyword.
	if deps, ok := sch["dependentRequired"].(map[string]any); ok {
		for _, key := range keysOf(deps) {
			if _, present := obj[key]; !present {
				continue
			}
			for _, other := range stringsOf(deps[key]) {
				if _, there := obj[other]; !there {
					v.fail(join(at, key), "only means something together with %q, and that is missing", other)
				}
			}
		}
	}

	// The one open object in the schema still has a rule about its keys.
	if names, ok := sch["propertyNames"].(map[string]any); ok {
		for _, key := range keysOf(obj) {
			if len(v.probe(join(at, key), key, names)) > 0 {
				v.fail(at, "%q is not a name that can go here%s", key, v.nameRule(names))
			}
		}
	}

	// An unknown key is nearly always a typo the author would never otherwise
	// see, so it is worth naming the alternatives.
	extra, _ := sch["additionalProperties"]
	if closed, isBool := extra.(bool); isBool && !closed && props != nil {
		var bad []string
		for k := range obj {
			if _, known := props[k]; !known {
				bad = append(bad, k)
			}
		}
		sort.Strings(bad)
		for _, k := range bad {
			v.fail(join(at, k), "is not a field here. Try one of: %s", strings.Join(keysOf(props), ", "))
		}
	}

	for k, child := range obj {
		if props != nil {
			if ps, ok := props[k].(map[string]any); ok {
				v.walk(join(at, k), child, ps)
				continue
			}
		}
		if ps, ok := extra.(map[string]any); ok {
			v.walk(join(at, k), child, ps)
		}
	}
}

func (v *validator) array(at string, arr []any, sch map[string]any) {
	if min, ok := numberOf(sch["minItems"]); ok && float64(len(arr)) < min {
		v.fail(at, "needs at least %d entr%s, and has %d", int(min), plural(int(min)), len(arr))
	}

	if unique, _ := sch["uniqueItems"].(bool); unique {
		seen := map[string]int{}
		for i, entry := range arr {
			k := valueKey(entry)
			if first, dup := seen[k]; dup {
				v.fail(fmt.Sprintf("%s[%d]", at, i), "says the same as [%d], and every entry here has to be different", first)
				continue
			}
			seen[k] = i
		}
	}

	items, ok := sch["items"].(map[string]any)
	if !ok {
		return
	}
	for i, entry := range arr {
		v.walk(fmt.Sprintf("%s[%d]", at, i), entry, items)
	}
}

func (v *validator) text(at, s string, sch map[string]any) {
	if min, ok := numberOf(sch["minLength"]); ok && float64(len([]rune(s))) < min {
		if min == 1 {
			v.fail(at, "is empty")
		} else {
			v.fail(at, "is shorter than %d characters", int(min))
		}
	}
	if src, ok := sch["pattern"].(string); ok {
		if p := v.doc.pats[src]; p != nil && !p.ok(s) {
			v.fail(at, "%q does not match %s", s, src)
		}
	}
}

func (v *validator) number(at string, n float64, sch map[string]any) {
	if min, ok := numberOf(sch["minimum"]); ok && n < min {
		v.fail(at, "must be %d or more", int(min))
	}
	if sch["type"] == "integer" && n != math.Trunc(n) {
		v.fail(at, "must be a whole number")
	}
}

// ---------------------------------------------------------------- applicators

func (v *validator) applicators(at string, value any, sch map[string]any) {
	for _, one := range branchesOf(sch["allOf"]) {
		v.walk(at, value, one)
	}
	if branches := branchesOf(sch["oneOf"]); len(branches) > 0 {
		v.oneOf(at, value, branches)
	}
	if branches := branchesOf(sch["anyOf"]); len(branches) > 0 {
		v.anyOf(at, value, branches)
	}
	if no, ok := sch["not"].(map[string]any); ok && len(v.probe(at, value, no)) == 0 {
		v.fail(at, "%s", forbids(no))
	}
	if cond, ok := sch["if"].(map[string]any); ok {
		branch := "else"
		if len(v.probe(at, value, cond)) == 0 {
			branch = "then"
		}
		if next, ok := sch[branch].(map[string]any); ok {
			v.walk(at, value, next)
		}
	}
}

// oneOf is how cw/2 says a block is one of seven kinds. Checking a value against
// all seven and reporting every failure buries the one line that matters under
// six about kinds the author never meant. So when the branches all pin the same
// field to a different constant, that field says which branch was meant, and
// only its complaints are reported.
func (v *validator) oneOf(at string, value any, branches []map[string]any) {
	if key, byValue := v.discriminator(branches); key != "" {
		obj, isObject := value.(map[string]any)
		if !isObject {
			v.fail(at, "should be an object with a %q, not %s", key, kindOf(value))
			return
		}
		kinds := strings.Join(sortedKeys(byValue), ", ")
		name, _ := obj[key].(string)
		if name == "" {
			v.fail(join(at, key), "is missing, and it is what says which kind this is. One of: %s", kinds)
			return
		}
		if branch, known := byValue[name]; known {
			v.walk(at, value, branch)
			return
		}
		v.fail(join(at, key), "%q is not one of: %s", name, kinds)
		return
	}

	// No discriminator, so fall back to the plain rule: exactly one branch has to
	// accept it. When none does, the branch that came closest is the one whose
	// complaints are worth reading.
	fits := 0
	var nearest []string
	for _, b := range branches {
		errs := v.probe(at, value, b)
		if len(errs) == 0 {
			fits++
			continue
		}
		if nearest == nil || len(errs) < len(nearest) {
			nearest = errs
		}
	}
	switch {
	case fits == 1:
	case fits > 1:
		v.fail(at, "fits %d of the shapes allowed here, and it has to fit exactly one", fits)
	default:
		v.errs = append(v.errs, nearest...)
	}
}

// discriminator looks for a property that every branch pins to its own constant.
func (v *validator) discriminator(branches []map[string]any) (string, map[string]map[string]any) {
	first := v.resolve(branches[0])
	props, _ := first["properties"].(map[string]any)
	for _, key := range keysOf(props) {
		byValue := map[string]map[string]any{}
		for _, b := range branches {
			resolved := v.resolve(b)
			p, _ := resolved["properties"].(map[string]any)
			field, _ := p[key].(map[string]any)
			name, isText := field["const"].(string)
			if !isText {
				break
			}
			if _, taken := byValue[name]; taken {
				break
			}
			byValue[name] = resolved
		}
		if len(byValue) == len(branches) {
			return key, byValue
		}
	}
	return "", nil
}

func (v *validator) anyOf(at string, value any, branches []map[string]any) {
	for _, b := range branches {
		if len(v.probe(at, value, b)) == 0 {
			return
		}
	}
	if names := requiredAcross(branches); len(names) > 0 {
		v.fail(at, "needs at least one of %s", strings.Join(quoteAll(names), ", "))
		return
	}
	v.fail(at, "does not fit any of the shapes allowed here")
}

// forbids turns a not back into the sentence an author can act on. Every not in
// these schemas is about fields that cannot be there, so naming them beats
// saying that some rule matched.
func forbids(sch map[string]any) string {
	names := requiredAnywhere(sch)
	if len(names) == 0 {
		return "is not allowed in this combination"
	}
	return "must not have " + strings.Join(quoteAll(names), " or ") + " here"
}

// requiredAcross is the union of the required fields when every branch is
// nothing but a required list, which is how a schema spells "one of these".
func requiredAcross(branches []map[string]any) []string {
	set := map[string]bool{}
	for _, b := range branches {
		if len(b) != 1 {
			return nil
		}
		req, ok := b["required"].([]any)
		if !ok {
			return nil
		}
		for _, r := range req {
			if s, ok := r.(string); ok {
				set[s] = true
			}
		}
	}
	return sortedSet(set)
}

func requiredAnywhere(sch any) []string {
	set := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if req, ok := n["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						set[s] = true
					}
				}
			}
			for _, child := range n {
				walk(child)
			}
		case []any:
			for _, child := range n {
				walk(child)
			}
		}
	}
	walk(sch)
	return sortedSet(set)
}

func (v *validator) nameRule(sch map[string]any) string {
	if src, ok := v.resolve(sch)["pattern"].(string); ok {
		return ". Names here match " + src
	}
	return ""
}

// ---------------------------------------------------------------- small helpers

func branchesOf(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if m, ok := entry.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func typeMatches(want string, value any) bool {
	switch want {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == math.Trunc(n)
	case "null":
		return value == nil
	}
	return true
}

func kindOf(value any) string {
	switch n := value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "a list"
	case string:
		return "text"
	case bool:
		return "true or false"
	case float64:
		if n == math.Trunc(n) {
			return "a whole number"
		}
		return "a number"
	}
	return "something else"
}

func join(at, key string) string {
	if at == "" {
		return key
	}
	return at + "." + key
}

func numberOf(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}

func containsValue(list []any, want any) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func joinValues(list []any) string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, fmt.Sprintf("%v", v))
	}
	return strings.Join(out, ", ")
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stringsOf(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		if s, ok := entry.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// valueKey is a value written the one way json.Marshal writes it, so two entries
// of a list can be compared without caring how they were typed.
func valueKey(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// dedupe exists because an allOf can walk the same field twice and an author
// does not want to read the same sentence twice.
func dedupe(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i > 0 && s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
