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

It reads the subset of JSON Schema the walkthrough schema actually uses: type,
properties, required, additionalProperties, items, enum, const, minimum,
minItems, minLength, pattern and local $ref. Anything beyond that is a keyword
nobody should reach for in this file, and it is skipped rather than guessed at.
*/

type schemaDoc struct {
	root map[string]any
}

func loadSchema(raw []byte) (*schemaDoc, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("the walkthrough schema itself is not valid JSON: %w", err)
	}
	return &schemaDoc{root: m}, nil
}

// Validate walks the document against the schema and returns every complaint,
// each one addressed by the path an author can find in their file.
func (s *schemaDoc) Validate(doc any) []string {
	v := &validator{doc: s}
	v.walk("", doc, s.root)
	sort.Strings(v.errs)
	return v.errs
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
		if min, ok := numberOf(sch["minLength"]); ok && float64(len([]rune(actual))) < min {
			if min == 1 {
				v.fail(at, "is empty")
			} else {
				v.fail(at, "is shorter than %d characters", int(min))
			}
		}
		if pat, ok := sch["pattern"].(string); ok {
			if re, err := regexp.Compile(pat); err == nil && !re.MatchString(actual) {
				v.fail(at, "%q does not match %s", actual, pat)
			}
		}
	case float64:
		if min, ok := numberOf(sch["minimum"]); ok && actual < min {
			v.fail(at, "must be %d or more", int(min))
		}
		if sch["type"] == "integer" && actual != math.Trunc(actual) {
			v.fail(at, "must be a whole number")
		}
	}
}

func (v *validator) object(at string, obj map[string]any, sch map[string]any) {
	props, _ := sch["properties"].(map[string]any)

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
	items, ok := sch["items"].(map[string]any)
	if !ok {
		return
	}
	for i, entry := range arr {
		v.walk(fmt.Sprintf("%s[%d]", at, i), entry, items)
	}
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
