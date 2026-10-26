// Command playctl is the developer and CI tool (backend.md §4).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/zsy/emacs-playground/internal/buildinfo"
	"github.com/zsy/emacs-playground/internal/config"
	"github.com/zsy/emacs-playground/internal/protocol/tsgen"
)

const usage = `usage: playctl <command> [args]

commands:
  curriculum validate [dir]            schema-check the curriculum source (default ./curriculum)
  curriculum build [dir] -o out/       build curriculum.json, lesson bundles and fixture tarballs
  curriculum play <lesson> [-session URL]
                                       replay every step's demo in a session and require completion
  config schema                        print the configuration JSON Schema
  config check <file>                  validate a configuration file
  protocol typescript [-o file]        generate TypeScript types from internal/protocol
  images build [session|gateway|remote-box]
  dev                                  run the gateway from source with the Vite dev server
  session shell                        attach to the local session's tmux
  export-user <id>                     tarball of a workspace and its progress (server)
  version                              print the version
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		if len(args) == 0 {
			os.Exit(2)
		}
		return
	}
	switch args[0] {
	case "version":
		fmt.Println(buildinfo.Version)
	case "config":
		if len(args) >= 3 && args[1] == "check" {
			if _, err := config.LoadFile(args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println(args[2] + ": ok")
			return
		}
		// TODO(playctl, backend.md §4): config schema via invopop/jsonschema.
		notYet(args)
	case "protocol":
		if len(args) >= 2 && args[1] == "typescript" {
			os.Exit(protocolTypescript(args[2:]))
		}
		notYet(args)
	default:
		// TODO(playctl, backend.md §4): curriculum validate/build/play,
		// images build, dev, session shell, export-user.
		notYet(args)
	}
}

// protocolTypescript implements `playctl protocol typescript [-o file]
// [-src dir] [-check]`. With -check it exits 1 when the file is out of date
// (CI drift check, frontend.md §14).
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

func notYet(args []string) {
	fmt.Fprintf(os.Stderr, "playctl %v: not implemented yet\n\n%s", args, usage)
	os.Exit(1)
}
