package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/floatshadow/emacs-playground/internal/protocol/tsgen"
)

// protocolTypescript implements `playctl protocol typescript [-o file]
// [-src dir] [-check]`. With -check it exits 1 when the file is out of date
// (CI drift check, frontend.md §14). Types come from internal/protocol/tsgen
// (api.md §8), not from JSON Schema.
func protocolTypescript(args []string) int {
	fs := flag.NewFlagSet("protocol typescript", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	src := fs.String("src", "internal/protocol", "protocol package directory")
	check := fs.Bool("check", false, "fail if the output file differs from the generated code")
	_ = fs.Parse(args)
	code, err := tsgen.Generate(*src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *out == "" {
		_, _ = os.Stdout.Write(code)
		return 0
	}
	if *check {
		cur, err := os.ReadFile(*out)
		if err != nil || !bytes.Equal(cur, code) {
			fmt.Fprintf(os.Stderr, "%s is out of date; run playctl protocol typescript -o %s\n", *out, *out)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(*out, code, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
