// Command gateway is the Emacs Playground gateway (backend.md §2).
//
//	gateway [flags]          run the server
//	gateway config [flags]   print the effective configuration (secrets redacted)
//	gateway healthcheck      exit 0 when GET /healthz on the configured listen address is 200
//	gateway version          print the version
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zsy/emacs-playground/internal/buildinfo"
	"github.com/zsy/emacs-playground/internal/config"
)

const usage = `usage: gateway [command] [flags]

commands:
  (none)        run the gateway
  config        print the effective configuration with secrets redacted
  healthcheck   exit 0 when the gateway answers GET /healthz (Compose health check)
  version       print the version

flags:
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage); fs.PrintDefaults() }
	flags := config.BindFlags(fs)
	_ = fs.Parse(args)

	switch cmd {
	case "version":
		fmt.Println(buildinfo.Version)
	case "config":
		cfg, err := config.Load(flags.Sources(os.Environ()))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(config.Redacted(cfg))
	case "healthcheck":
		// TODO(gateway, deployment.md §2): GET http://127.0.0.1<listen>/healthz.
		fmt.Fprintln(os.Stderr, "gateway healthcheck: not implemented yet")
		os.Exit(1)
	case "":
		if _, err := config.Load(flags.Sources(os.Environ())); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// TODO(gateway, backend.md §2): wire internal/gateway and run.
		fmt.Fprintln(os.Stderr, "gateway: server not implemented yet")
		os.Exit(1)
	default:
		fs.Usage()
		os.Exit(2)
	}
}
