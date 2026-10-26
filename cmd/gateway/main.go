// Command gateway is the Emacs Playground gateway (backend.md §2).
//
//	gateway [flags]          run the server
//	gateway config [flags]   print the effective configuration (secrets redacted)
//	gateway healthcheck      exit 0 when GET /healthz on the configured listen address is 200
//	gateway version          print the version
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zsy/emacs-playground/internal/buildinfo"
	"github.com/zsy/emacs-playground/internal/config"
	"github.com/zsy/emacs-playground/internal/gateway"
	"github.com/zsy/emacs-playground/internal/protocol"
	"github.com/zsy/emacs-playground/web"
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
	webDir := fs.String("web", os.Getenv("PG_WEB_DIR"), "serve the web app from this directory instead of the embedded bundle")
	fontsDir := fs.String("fonts", os.Getenv("PG_FONTS_DIR"), "serve /fonts from this directory (default /srv/fonts or web/public/fonts)")
	_ = fs.Parse(args)

	switch cmd {
	case "version":
		fmt.Println(buildinfo.Version)
	case "config":
		cfg := load(flags)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(config.Redacted(cfg))
	case "healthcheck":
		os.Exit(healthcheck(load(flags)))
	case "":
		os.Exit(run(load(flags), *webDir, *fontsDir))
	default:
		fs.Usage()
		os.Exit(2)
	}
}

func load(flags *config.Flags) *protocol.Config {
	cfg, err := config.Load(flags.Sources(os.Environ()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return cfg
}

func newLogger(cfg *protocol.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.Logging.Level))
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.Logging.Format == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h).With("service", "gateway")
}

func run(cfg *protocol.Config, webDir, fontsDir string) int {
	log := newLogger(cfg)
	slog.SetDefault(log)
	log.Info("gateway: configuration", "config", config.Redacted(cfg))

	var dist fs.FS
	if webDir != "" {
		d, err := gateway.WebDir(webDir)
		if err != nil {
			log.Error("gateway", "err", err)
			return 1
		}
		if d == nil {
			log.Warn("gateway: -web directory does not exist; serving the placeholder", "dir", webDir)
		}
		dist = d
	} else if d, ok := web.Dist(); ok {
		dist = d
	} else {
		log.Warn("gateway: built without the web bundle (-tags embedweb); serving a placeholder at /")
	}
	fonts := gateway.FontsDir(fontsDir, dist)
	if fonts == nil {
		log.Warn("gateway: no fonts directory; /fonts answers 404 (the app falls back to local fonts)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := gateway.Build(ctx, cfg, gateway.Options{Web: dist, Fonts: fonts, Logger: log})
	if err != nil {
		log.Error("gateway: startup failed", "err", err)
		return 1
	}
	defer app.Close()
	if err := app.Run(ctx); err != nil {
		log.Error("gateway", "err", err)
		return 1
	}
	return 0
}

// healthcheck implements `gateway healthcheck` (deployment.md §2): GET
// /healthz on the configured listen address.
func healthcheck(cfg *protocol.Config) int {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(strings.Trim(host, "[]"), port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck:", resp.Status)
		return 1
	}
	return 0
}
