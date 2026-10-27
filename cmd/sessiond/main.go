// Command sessiond runs inside each session container (backend.md §3).
//
//	sessiond [flags]             run the daemon (configured by PG_* environment variables)
//	sessiond [flags] config      print the effective configuration (secrets redacted)
//	sessiond [flags] healthcheck exit 0 when /healthz on PG_LISTEN is 200 (Compose health check)
//	sessiond version             print the version
//
// Every PG_* variable has a flag named after its JSON key (-listen,
// -workspace, -gatewayUrl, …); flags override the environment. -fake
// substitutes bash for Emacs, tmux and Xvnc and runs an in-process fake agent
// (development on hosts without them).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/floatshadow/emacs-playground/internal/buildinfo"
	"github.com/floatshadow/emacs-playground/internal/config"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond"
)

const usage = `usage: sessiond [flags] [command]

commands:
  (none)        run sessiond (configuration from PG_* environment variables, deployment.md §3)
  config        print the effective configuration with secrets redacted
  healthcheck   exit 0 when the HTTP server is up and the agent has said hello
  version       print the version

flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Environ(), os.Stdout, os.Stderr))
}

// configFlags registers one string flag per SessiondConfig field, named
// after its JSON key, and returns the environment overrides they produce.
func configFlags(fs *flag.FlagSet) func() []string {
	t := reflect.TypeOf(protocol.SessiondConfig{})
	envOf := map[string]string{} // flag name → variable
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		env := f.Tag.Get("env")
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if env == "" || name == "" {
			continue
		}
		envOf[name] = env
		fs.String(name, "", fmt.Sprintf("overrides %s (default %q)", env, f.Tag.Get("default")))
	}
	return func() []string {
		var out []string
		fs.Visit(func(fl *flag.Flag) {
			if env, ok := envOf[fl.Name]; ok {
				out = append(out, env+"="+fl.Value.String())
			}
		})
		return out
	}
}

func run(args, environ []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sessiond", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fake := fs.Bool("fake", false, "substitute bash for Emacs, tmux and Xvnc (development without the session image)")
	overrides := configFlags(fs)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	env := append(append([]string{}, environ...), overrides()...)
	env = defaultAgentSocket(env)
	cmd := fs.Arg(0)

	switch cmd {
	case "version":
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	case "", "config", "healthcheck":
	default:
		fs.Usage()
		return 2
	}
	cfg, err := config.LoadSessiond(env)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch cmd {
	case "config":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(config.RedactedSessiond(cfg))
		return 0
	case "healthcheck":
		if err := healthcheck(cfg.Listen, 2*time.Second); err != nil {
			fmt.Fprintln(stderr, "sessiond healthcheck:", err)
			return 1
		}
		return 0
	}
	log := newLogger(cfg, stdout)
	slog.SetDefault(log)
	log.Info("sessiond starting", "version", buildinfo.Version, "session", cfg.SessionID, "listen", cfg.Listen, "workspace", cfg.Workspace)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	srv := sessiond.New(cfg, sessiond.Options{Fake: *fake, Log: log})
	if err := srv.Run(ctx); err != nil {
		log.Error("sessiond failed", "err", err)
		return 1
	}
	log.Info("sessiond stopped")
	return 0
}

// defaultAgentSocket puts the agent socket inside PG_RUN_DIR when only the
// run directory was moved (development on a host).
func defaultAgentSocket(env []string) []string {
	runDir := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "PG_AGENT_SOCKET=") {
			return env
		}
		if v, ok := strings.CutPrefix(kv, "PG_RUN_DIR="); ok {
			runDir = v
		}
	}
	if runDir == "" {
		return env
	}
	return append(env, "PG_AGENT_SOCKET="+strings.TrimRight(runDir, "/")+"/agent.sock")
}

func newLogger(cfg *protocol.SessiondConfig, w io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h).With("session_id", cfg.SessionID)
}

// healthAddr turns a listen address into one to dial: ":7000" and
// "0.0.0.0:7000" become 127.0.0.1:7000.
func healthAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// healthcheck is the Compose health command (backend.md §3.13).
func healthcheck(listen string, timeout time.Duration) error {
	c := &http.Client{Timeout: timeout}
	resp, err := c.Get("http://" + healthAddr(listen) + protocol.PathHealthz)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var h protocol.HealthStatus
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&h)
		return fmt.Errorf("%s: %s", resp.Status, h.Reason)
	}
	return nil
}
