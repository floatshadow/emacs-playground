package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// The conformance corpus (testdata/conformance) pins the semantics the
// browser's ports share with this package: web/src/advice/conformance.test.ts
// runs the same files. layouts.json has no port and runs here only.

// conformance decodes testdata/conformance/<name>.json into a list of T.
func conformance[T any](t *testing.T, name string) []T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "conformance", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

// conformanceDoc is the case's doc, or StateDoc of state.json with State.
func conformanceDoc(t *testing.T, state bool, doc any) any {
	t.Helper()
	if !state {
		return doc
	}
	b, err := os.ReadFile(filepath.Join("testdata", "conformance", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return StateDoc(&s)
}

func TestConformancePredicates(t *testing.T) {
	for _, c := range conformance[struct {
		Name      string
		Predicate Predicate
		State     bool
		Doc       any
		Want      bool
		Error     bool
	}](t, "predicates") {
		t.Run(c.Name, func(t *testing.T) {
			err := ValidatePredicate(c.Predicate)
			if c.Error || err != nil {
				if c.Error != (err != nil) {
					t.Fatalf("validate: %v, want error %v", err, c.Error)
				}
				return
			}
			if got := c.Predicate.Match(conformanceDoc(t, c.State, c.Doc)); got != c.Want {
				t.Fatalf("match %v, want %v", got, c.Want)
			}
		})
	}
}

func TestConformanceTemplates(t *testing.T) {
	for _, c := range conformance[struct {
		Name, Template, Want string
		Predicate            Predicate
		State                bool
		Doc                  any
	}](t, "templates") {
		if got := ExpandTemplate(c.Template, conformanceDoc(t, c.State, c.Doc), c.Predicate); got != c.Want {
			t.Errorf("%s: got %q, want %q", c.Name, got, c.Want)
		}
	}
}

func TestConformanceDemos(t *testing.T) {
	for _, c := range conformance[struct {
		Name, Demo string
		Want       []DemoToken
		Error      bool
	}](t, "demos") {
		got, err := ParseDemo(c.Demo)
		if (err != nil) != c.Error || !c.Error && !reflect.DeepEqual(got, c.Want) {
			t.Errorf("%s: ParseDemo(%q) = %+v, %v", c.Name, c.Demo, got, err)
		}
	}
}

func TestConformanceKeys(t *testing.T) {
	for _, c := range conformance[struct {
		Name, Keys, Want string
		Error            bool
	}](t, "keys") {
		got, err := NormalizeKeys(c.Keys)
		if (err != nil) != c.Error || got != c.Want {
			t.Errorf("%s: NormalizeKeys(%q) = %q, %v", c.Name, c.Keys, got, err)
		}
	}
}

func TestConformanceLayouts(t *testing.T) {
	for _, c := range conformance[struct {
		Name, Pattern string
		Layout        Layout
		Bufs          map[string]string
		Want, Error   bool
	}](t, "layouts") {
		p, err := ParseLayoutPattern(c.Pattern)
		if c.Error || err != nil {
			if c.Error != (err != nil) {
				t.Errorf("%s: ParseLayoutPattern(%q): %v", c.Name, c.Pattern, err)
			}
			continue
		}
		bufOf := func(id int) string { return c.Bufs[strconv.Itoa(id)] }
		if got := MatchLayout(p, c.Layout, bufOf); got != c.Want {
			t.Errorf("%s: MatchLayout = %v, want %v", c.Name, got, c.Want)
		}
	}
}
