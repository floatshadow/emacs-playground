package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zsy/emacs-playground/internal/curriculum"
)

// Compose files of local mode and the dev overlay (deployment.md §2, §4).
var (
	localCompose = []string{"-f", "compose.yaml", "-f", "compose.local.yaml"}
	devCompose   = append(append([]string(nil), localCompose...), "-f", "compose.dev.yaml")
)

const imageRepo = "ghcr.io/zsy/emacs-playground/"

// dev implements `playctl dev` (backend.md §4, frontend.md §15): the session
// container on the host network, the curriculum built into .dev/curriculum,
// the gateway from source with deploy/config.dev.json and the Vite dev
// server on :5173. Ctrl-C stops the gateway and Vite; the session container
// keeps running (docker compose stop session).
func dev(args []string) int {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	noSession := fs.Bool("no-session", false, "do not start the session container")
	configFile := fs.String("config", "deploy/config.dev.json", "gateway configuration")
	_ = fs.Parse(args)
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(os.Stderr, "playctl dev: run it from the repository root")
		return 2
	}
	for _, d := range []string{"workspace", ".dev/data"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	// The session runs as uid 1000 whatever the host uid is (AGENTS.md §3).
	_ = os.Chmod("workspace", 0o777)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !*noSession {
		if err := runCmd(ctx, "", "docker", append(append([]string{"compose"}, devCompose...), "up", "-d", "session")...); err != nil {
			fmt.Fprintln(os.Stderr, "playctl dev: starting the session container:", err)
			return 1
		}
	}
	src, diags := curriculum.Load("curriculum")
	for _, d := range diags {
		if d.Severity == curriculum.Error {
			fmt.Fprintln(os.Stderr, d)
		}
	}
	if diags.HasErrors() {
		dropped := dropBrokenLessons(src, diags)
		fmt.Fprintf(os.Stderr, "playctl dev: curriculum has errors; serving without %v\n", dropped)
	}
	if err := curriculum.Build(src, filepath.Join(".dev", "curriculum"), curriculum.BuildOptions{Version: "dev"}); err != nil {
		fmt.Fprintln(os.Stderr, "playctl dev: building the curriculum:", err)
		return 1
	}

	gw := exec.CommandContext(ctx, "go", "run", "./cmd/gateway", "-config", *configFile)
	vite := exec.CommandContext(ctx, "pnpm", "dev")
	vite.Dir = "web"
	vite.Env = append(os.Environ(), "COREPACK_ENABLE_DOWNLOAD_PROMPT=0")
	for _, c := range []*exec.Cmd{gw, vite} {
		c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, nil
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		c.Cancel = func() error { return killGroup(c) }
		c.WaitDelay = 5 * time.Second
		if err := c.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "playctl dev:", err)
			stop()
			return 1
		}
	}
	fmt.Fprintln(os.Stderr, "playctl dev: gateway on 127.0.0.1:8080, app on http://localhost:5173 (Ctrl-C stops both)")
	done := make(chan error, 2)
	go func() { done <- gw.Wait() }()
	go func() { done <- vite.Wait() }()
	err := <-done
	stop()
	<-done
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "playctl dev:", err)
		return 1
	}
	return 0
}

func killGroup(c *exec.Cmd) error {
	if c.Process == nil {
		return nil
	}
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGTERM); err != nil {
		return c.Process.Kill()
	}
	return nil
}

// sessionShell implements `playctl session shell`: attach the terminal to
// the local session's tmux (make shell). The browser shows "attached
// elsewhere" meanwhile.
func sessionShell(args []string) int {
	fs := flag.NewFlagSet("session shell", flag.ExitOnError)
	devMode := fs.Bool("dev", false, "the session was started by playctl dev / make dev")
	_ = fs.Parse(args)
	files := localCompose
	if *devMode {
		files = devCompose
	}
	cmd := append(append([]string{"compose"}, files...), "exec", "session", "tmux", "-L", "pg", "attach-session", "-t", "emacs")
	if err := runCmd(context.Background(), "", "docker", cmd...); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// imagesBuild implements `playctl images build [session|gateway|remote-box]`
// with plain docker build. TODO(playctl, deployment.md §1): docker buildx
// bake with a registry cache once a bake file exists.
func imagesBuild(args []string) int {
	fs := flag.NewFlagSet("images build", flag.ExitOnError)
	tag := fs.String("tag", "dev", "image tag")
	version := fs.String("version", "", "VERSION build argument (default: the tag)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		pos = []string{"gateway", "session"}
	}
	if *version == "" {
		*version = *tag
	}
	for _, name := range pos {
		switch name {
		case "gateway", "session", "remote-box":
		default:
			fmt.Fprintf(os.Stderr, "unknown image %q (gateway, session, remote-box)\n", name)
			return 2
		}
		err := runCmd(context.Background(), "", "docker", "build",
			"-f", filepath.Join("images", name, "Dockerfile"),
			"--build-arg", "VERSION="+*version,
			"-t", imageRepo+name+":"+*tag, ".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "building %s: %v\n", name, err)
			return 1
		}
	}
	return 0
}

// fonts implements `playctl fonts [-o .dev/fonts]`: builds the gateway
// image's fonts stage (pinned, checksummed Iosevka Nerd Font and Source
// Serif 4 WOFF2 files) and copies them out, so a gateway started from
// source (playctl dev, make dev) serves /fonts like the image does.
func fonts(args []string) int {
	fs := flag.NewFlagSet("fonts", flag.ExitOnError)
	out := fs.String("o", filepath.Join(".dev", "fonts"), "output directory")
	_ = fs.Parse(args)
	ctx := context.Background()
	tag := "emacs-playground-fonts:local"
	if err := runCmd(ctx, "", "docker", "build", "-q", "-f", filepath.Join("images", "gateway", "Dockerfile"), "--target", "fonts", "-t", tag, "."); err != nil {
		fmt.Fprintln(os.Stderr, "playctl fonts: building the fonts stage:", err)
		return 1
	}
	name := fmt.Sprintf("pg-fonts-%d", os.Getpid())
	if err := exec.Command("docker", "create", "--name", name, tag, "true").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "playctl fonts: docker create:", err)
		return 1
	}
	defer exec.Command("docker", "rm", "-f", name).Run()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// docker cp extracts on the client side, so the files belong to the
	// invoking user.
	if err := runCmd(ctx, "", "docker", "cp", name+":/out/fonts/.", *out); err != nil {
		fmt.Fprintln(os.Stderr, "playctl fonts: docker cp:", err)
		return 1
	}
	fmt.Println("fonts in", *out)
	return 0
}

func runCmd(ctx context.Context, dir, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
