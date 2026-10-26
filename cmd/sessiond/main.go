// Command sessiond runs inside each session container (backend.md §3).
//
//	sessiond                run the daemon (configured by PG_* environment variables)
//	sessiond config         print the effective configuration (secrets redacted)
//	sessiond healthcheck    exit 0 when /healthz on PG_LISTEN is 200 (Compose health check)
//	sessiond version        print the version
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zsy/emacs-playground/internal/buildinfo"
	"github.com/zsy/emacs-playground/internal/config"
)

const usage = `usage: sessiond [command]

commands:
  (none)        run sessiond (configuration from PG_* environment variables, deployment.md §3)
  config        print the effective configuration with secrets redacted
  healthcheck   exit 0 when the HTTP server is up and the agent has said hello
  version       print the version
`

func main() {
	fs := flag.NewFlagSet("sessiond", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage); fs.PrintDefaults() }
	_ = fs.Parse(os.Args[1:])
	cmd := fs.Arg(0)

	switch cmd {
	case "version":
		fmt.Println(buildinfo.Version)
	case "config":
		cfg, err := config.LoadSessiond(os.Environ())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(config.RedactedSessiond(cfg))
	case "healthcheck":
		// TODO(sessiond, backend.md §3.13): GET http://127.0.0.1<PG_LISTEN>/healthz.
		fmt.Fprintln(os.Stderr, "sessiond healthcheck: not implemented yet")
		os.Exit(1)
	case "":
		if _, err := config.LoadSessiond(os.Environ()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// TODO(sessiond, backend.md §3): wire internal/sessiond and run.
		fmt.Fprintln(os.Stderr, "sessiond: not implemented yet")
		os.Exit(1)
	default:
		fs.Usage()
		os.Exit(2)
	}
}
