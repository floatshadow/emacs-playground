package tsgen

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	code, err := Generate("..")
	if err != nil {
		t.Fatal(err)
	}
	s := string(code)
	for _, want := range []string{
		`export type Event = HelloEvent | KeyEvent |`,
		`export interface State {`,
		`  t: "state";`,
		`  file: string | null;`,
		`export type FrameKind = "terminal" | "gui";`,
		`export const FrameTerminal = "terminal" satisfies FrameKind;`,
		`  "restart-emacs": { request: Empty; result: Empty };`,
		`export type Layout = number | [SplitKind, ...Layout[]] | null;`,
		`export interface Config {`,
		`  $schema?: string;`,
		`export const CloseReplaced = 4000;`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated code lacks %q", want)
		}
	}
}

// TestCheckedInFileUpToDate fails when web/src/protocol.gen.ts drifts from
// internal/protocol (frontend.md §14). Fix: go run ./cmd/playctl protocol
// typescript -o web/src/protocol.gen.ts
func TestCheckedInFileUpToDate(t *testing.T) {
	cur, err := os.ReadFile("../../../web/src/protocol.gen.ts")
	if err != nil {
		t.Skip("web/src/protocol.gen.ts not present")
	}
	code, err := Generate("..")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cur, code) {
		t.Fatal("web/src/protocol.gen.ts is out of date: run go run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts")
	}
}
