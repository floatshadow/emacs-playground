// Command playctl is the developer and CI tool (backend.md §4).
//
//	playctl curriculum validate [dir]
//	playctl curriculum build [dir] -o out/
//	playctl curriculum play <lesson>... | all [-session URL] [-dir curriculum]
//	playctl config schema | check <file>
//	playctl protocol typescript [-o file] [-check]
//	playctl dev | session shell | images build [image]
//	playctl version
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zsy/emacs-playground/internal/buildinfo"
)

const usage = `usage: playctl <command> [args]

commands:
  curriculum validate [dir]            schema-check the curriculum source (default ./curriculum)
  curriculum build [dir] -o out/       build curriculum.json, lesson bundles and fixture tarballs
  curriculum play <lesson>... | all [-session URL] [-dir curriculum] [-timeout 30s] [-speed 1]
                                       replay every step's demo in a session and require completion
  config schema                        print the configuration JSON Schema
  config check <file>                  validate a configuration file
  protocol typescript [-o file] [-check]
                                       generate TypeScript types from internal/protocol
  images build [session|gateway|remote-box] [-tag T]
                                       build images with docker build
  dev [-no-session]                    session container, gateway from source and the Vite dev server
  fonts [-o .dev/fonts]                web fonts from the gateway image's fonts stage, for dev
  session shell                        attach your terminal to the local session's tmux
  export-user <id>                     tarball of a workspace and its progress (server; not yet)
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
	os.Exit(run(args))
}

func run(args []string) int {
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	switch args[0] {
	case "version":
		fmt.Println(buildinfo.Version)
		return 0
	case "curriculum":
		switch sub {
		case "validate":
			return curriculumValidate(args[2:])
		case "build":
			return curriculumBuild(args[2:])
		case "play":
			return curriculumPlay(args[2:])
		}
	case "config":
		switch sub {
		case "schema":
			return configSchema(args[2:])
		case "check":
			return configCheck(args[2:])
		}
	case "protocol":
		if sub == "typescript" {
			return protocolTypescript(args[2:])
		}
	case "images":
		if sub == "build" {
			return imagesBuild(args[2:])
		}
	case "dev":
		return dev(args[1:])
	case "fonts":
		return fonts(args[1:])
	case "session":
		if sub == "shell" {
			return sessionShell(args[2:])
		}
	case "export-user":
		// TODO(playctl, deployment.md §5): tar the learner's workspace volume
		// (docker run --rm -v pg-ws-<uid>:/work …) together with their
		// progress rows from the gateway store.
		fmt.Fprintln(os.Stderr, "playctl export-user: not implemented in this build (deployment.md §5)")
		return 1
	}
	fmt.Fprintf(os.Stderr, "playctl %s: unknown command\n\n%s", strings.Join(args, " "), usage)
	return 2
}

// parseInterleaved parses flags that may appear before, between or after
// positional arguments ("play 0.1 -session URL") and returns the
// positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}
