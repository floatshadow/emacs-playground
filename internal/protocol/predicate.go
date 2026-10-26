package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Predicate maps dotted paths into a JSON document (normally the State
// object, see StateDoc) to expectations; it holds when every entry holds.
// Used by exit-table rows (ExitRow.When) and state conditions (StateCond).
//
// Paths: dot-separated object keys and array indices ("minibuffer.kind",
// "windows.0.buf"). Special segments:
//   - "length" on an array or string yields its length ("windows.length").
//   - A non-index segment applied to an array maps over the elements; the
//     expectation then holds if it holds for ANY element
//     ("otherWindows.mode").
//
// StateDoc adds the virtual roots "otherWindows" (non-selected windows) and
// "selectedWindow".
//
// Expectations:
//   - a scalar (string, number, bool) or null: equality (numbers compare as
//     float64; a missing path equals null).
//   - an array: membership, shorthand for {in: [...]}.
//   - an object with exactly one operator key:
//     {in: [...]}, {not: v} (negates equality or a nested operator object),
//     {gt: n}, {gte: n}, {lt: n}, {lte: n}, {exists: bool},
//     {contains: v} (substring of a string or element of an array),
//     {regex: "re"} (RE2 match on a string).
//
// YAML example (tables/exits.yaml):
//
//	when: { "otherWindows.mode": { in: [help-mode, dired-mode] } }
//	when: { narrowed: true }
//	when: { "windows.length": { gte: 3 } }
type Predicate map[string]any

// Predicate operators.
var predicateOps = map[string]bool{
	"in": true, "not": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"exists": true, "contains": true, "regex": true,
}

// ValidatePredicate checks operator objects and regular expressions.
func ValidatePredicate(p Predicate) error {
	for path, want := range p {
		if path == "" {
			return fmt.Errorf("predicate: empty path")
		}
		if err := validateExpectation(want); err != nil {
			return fmt.Errorf("predicate %q: %w", path, err)
		}
	}
	return nil
}

func validateExpectation(want any) error {
	m, ok := want.(map[string]any)
	if !ok {
		return nil
	}
	if len(m) != 1 {
		return fmt.Errorf("operator object must have exactly one key, has %d", len(m))
	}
	for op, arg := range m {
		if !predicateOps[op] {
			return fmt.Errorf("unknown operator %q", op)
		}
		switch op {
		case "in":
			if _, ok := arg.([]any); !ok {
				return fmt.Errorf("in: want a list")
			}
		case "gt", "gte", "lt", "lte":
			if _, ok := toFloat(arg); !ok {
				return fmt.Errorf("%s: want a number", op)
			}
		case "exists":
			if _, ok := arg.(bool); !ok {
				return fmt.Errorf("exists: want true or false")
			}
		case "regex":
			s, ok := arg.(string)
			if !ok {
				return fmt.Errorf("regex: want a string")
			}
			if _, err := regexp.Compile(s); err != nil {
				return fmt.Errorf("regex: %w", err)
			}
		case "not":
			return validateExpectation(arg)
		}
	}
	return nil
}

// Match evaluates p against doc (a decoded JSON value, normally StateDoc).
func (p Predicate) Match(doc any) bool {
	for path, want := range p {
		vals, found := Resolve(doc, path)
		if !matchAny(vals, found, want) {
			return false
		}
	}
	return true
}

// StateDoc converts s to the generic document predicates and templates
// evaluate against, adding the virtual roots otherWindows and
// selectedWindow.
func StateDoc(s *State) map[string]any {
	b, _ := json.Marshal(s)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc == nil {
		doc = map[string]any{}
	}
	others := []any{}
	if ws, ok := doc["windows"].([]any); ok {
		for _, w := range ws {
			if m, ok := w.(map[string]any); ok {
				if sel, _ := m["selected"].(bool); sel {
					doc["selectedWindow"] = m
					continue
				}
			}
			others = append(others, w)
		}
	}
	doc["otherWindows"] = others
	return doc
}

// Resolve returns the values at path in doc. found is false when the path
// does not exist. Array mapping (see Predicate) can yield several values.
func Resolve(doc any, path string) (vals []any, found bool) {
	cur := []any{doc}
	for _, seg := range strings.Split(path, ".") {
		var next []any
		for _, v := range cur {
			next = append(next, step(v, seg)...)
		}
		if len(next) == 0 {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func step(v any, seg string) []any {
	switch x := v.(type) {
	case map[string]any:
		if c, ok := x[seg]; ok {
			return []any{c}
		}
		return nil
	case []any:
		if seg == "length" {
			return []any{float64(len(x))}
		}
		if i, err := strconv.Atoi(seg); err == nil {
			if i >= 0 && i < len(x) {
				return []any{x[i]}
			}
			return nil
		}
		var out []any
		for _, e := range x {
			out = append(out, step(e, seg)...)
		}
		return out
	case string:
		if seg == "length" {
			return []any{float64(len([]rune(x)))}
		}
	}
	return nil
}

func matchAny(vals []any, found bool, want any) bool {
	if m, ok := want.(map[string]any); ok && len(m) == 1 {
		for op, arg := range m {
			if op == "exists" {
				b, _ := arg.(bool)
				return found == b
			}
			if op == "not" {
				return !matchAny(vals, found, arg)
			}
		}
	}
	if !found {
		return want == nil
	}
	for _, v := range vals {
		if matchOne(v, want) {
			return true
		}
	}
	return false
}

func matchOne(v any, want any) bool {
	switch w := want.(type) {
	case []any:
		return inList(v, w)
	case map[string]any:
		if len(w) != 1 {
			return false
		}
		for op, arg := range w {
			switch op {
			case "in":
				l, _ := arg.([]any)
				return inList(v, l)
			case "not":
				return !matchOne(v, arg)
			case "gt", "gte", "lt", "lte":
				a, ok1 := toFloat(v)
				b, ok2 := toFloat(arg)
				if !ok1 || !ok2 {
					return false
				}
				switch op {
				case "gt":
					return a > b
				case "gte":
					return a >= b
				case "lt":
					return a < b
				default:
					return a <= b
				}
			case "contains":
				switch x := v.(type) {
				case string:
					s, ok := arg.(string)
					return ok && strings.Contains(x, s)
				case []any:
					return inList(arg, x)
				}
				return false
			case "regex":
				s, _ := v.(string)
				re, err := regexp.Compile(fmt.Sprint(arg))
				return err == nil && re.MatchString(s)
			case "exists":
				b, _ := arg.(bool)
				return b
			}
		}
		return false
	default:
		return equal(v, want)
	}
}

func inList(v any, l []any) bool {
	for _, e := range l {
		if equal(v, e) {
			return true
		}
	}
	return false
}

func equal(a, b any) bool {
	if fa, ok := toFloat(a); ok {
		if fb, ok := toFloat(b); ok {
			return fa == fb
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

var templateRe = regexp.MustCompile(`\{([A-Za-z0-9_.]+)\}`)

// ExpandTemplate replaces {path} placeholders in tmpl with values resolved
// against doc. When the row's predicate p constrains a path that maps over
// an array (for example "otherWindows.mode"), placeholders under the same
// array root ("otherWindows.buf") take the first element that satisfies the
// predicate; otherwise the first value is used. Missing values become "".
func ExpandTemplate(tmpl string, doc any, p Predicate) string {
	return templateRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		path := m[1 : len(m)-1]
		if v, ok := resolveForTemplate(doc, path, p); ok {
			return formatValue(v)
		}
		return ""
	})
}

func resolveForTemplate(doc any, path string, p Predicate) (any, bool) {
	segs := strings.Split(path, ".")
	// Find an array root shared with a predicate path.
	for i := 1; i < len(segs); i++ {
		root := strings.Join(segs[:i], ".")
		arr, ok := Resolve(doc, root)
		if !ok || len(arr) != 1 {
			continue
		}
		list, ok := arr[0].([]any)
		if !ok {
			continue
		}
		rest := strings.Join(segs[i:], ".")
		for _, el := range list {
			if elementSatisfies(el, root, p) {
				if v, ok := Resolve(el, rest); ok && len(v) > 0 {
					return v[0], true
				}
			}
		}
	}
	v, ok := Resolve(doc, path)
	if !ok || len(v) == 0 {
		return nil, false
	}
	return v[0], true
}

func elementSatisfies(el any, root string, p Predicate) bool {
	prefix := root + "."
	for path, want := range p {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		vals, found := Resolve(el, strings.TrimPrefix(path, prefix))
		if !matchAny(vals, found, want) {
			return false
		}
	}
	return true
}

func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
