package sessiond

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent/fakeeditor"
)

// The environment of TestKilledSessiondChild: its directory (workspace
// and address file), its run directory, the gateway, and whether
// restore-editor holds.
const (
	killDirEnv  = "PG_TEST_KILL_DIR"
	killRunEnv  = "PG_TEST_KILL_RUN"
	killGwEnv   = "PG_TEST_KILL_GATEWAY"
	killHoldEnv = "PG_TEST_KILL_HOLD"
)

// TestKilledSessiondChild is the sessiond process TestKilledDuringRestore
// starts and kills; it runs only as that test's child. With killHoldEnv,
// restore-editor writes restoring.marker and never answers: the restore
// stops after its files, where the parent kills the process.
func TestKilledSessiondChild(t *testing.T) {
	dir := os.Getenv(killDirEnv)
	if dir == "" {
		t.Skip("runs as the child of TestKilledDuringRestore")
	}
	ws := filepath.Join(dir, "work")
	ed := fakeeditor.New(ws)
	if os.Getenv(killHoldEnv) != "" {
		ed.SetFault(protocol.AgentOpRestoreEditor, fakeeditor.Fault{Hook: func() {
			_ = os.WriteFile(filepath.Join(dir, "restoring.marker"), nil, 0o644)
			select {}
		}})
	}
	ts := startServer(t, ws, os.Getenv(killRunEnv), nil, os.Getenv(killGwEnv), func(o *Options) {
		o.Editor = ed
		o.Debounce = 10 * time.Millisecond
	})
	if err := os.WriteFile(filepath.Join(dir, "addr"), []byte(ts.base), 0o644); err != nil {
		t.Fatal(err)
	}
	select {}
}

// startKillChild starts TestKilledSessiondChild in its own process group
// and waits for its address.
func startKillChild(t *testing.T, dir, gwURL string, hold bool) (*testServer, *exec.Cmd) {
	t.Helper()
	_ = os.Remove(filepath.Join(dir, "addr"))
	// A fresh run directory, as a restarted container has; short, for the
	// agent socket's path.
	run, err := os.MkdirTemp("", "pgkill")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledSessiondChild$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), killDirEnv+"="+dir, killRunEnv+"="+run, killGwEnv+"="+gwURL)
	if hold {
		cmd.Env = append(cmd.Env, killHoldEnv+"=1")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if testing.Verbose() {
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killGroup(cmd) })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "addr")); err == nil && len(b) > 0 {
			return &testServer{t: t, base: string(b)}, cmd
		}
		if time.Now().After(deadline) {
			t.Fatal("the sessiond child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// killGroup kills the child and its processes (the fake daemon's bash)
// with SIGKILL: nothing of sessiond's shutdown runs.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
}

// sessiond is killed (SIGKILL, no shutdown) during a Reset, after git
// restored the files and before the editor state, the activation and
// state.json followed. The next sessiond converges: state.json names the
// step, the resume activates it, Reset restores its checkpoint again, and
// the learner completes the step.
func TestKilledDuringRestore(t *testing.T) {
	if os.Getenv(killDirEnv) != "" {
		t.Skip("the child runs TestKilledSessiondChild only")
	}
	for _, bin := range []string{"bash", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "pristine"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "pristine", "init.el"), []byte(";; init\n"), 0o644)
	gw := &fakeGateway{lessons: lifecycleLessons(t), tarball: makeTarball(t, map[string]string{"README.md": "# hello\n"})}
	gwSrv := httptest.NewServer(gw.handler(t))
	t.Cleanup(gwSrv.Close)
	bTxt := filepath.Join(dir, "work", "life", "b.txt")

	ts, child := startKillChild(t, dir, gwSrv.URL, true)
	if code := ts.rpc("scenario", protocol.ScenarioRequest{LessonID: lessonA}, nil); code != 200 {
		t.Fatalf("scenario %d", code)
	}
	playKeys(t, ts, demoOpen)
	waitStep(t, ts, "write")
	playKeys(t, ts, `"xx" C-x C-s`)
	waitFor(t, "the learner's save", func() bool { b, _ := os.ReadFile(bTxt); return strings.Contains(string(b), "xx") })

	go func() { _ = ts.rpcCtx(context.Background(), "restore", protocol.RestoreRequest{}) }()
	waitFor(t, "restore-editor", func() bool {
		_, err := os.Stat(filepath.Join(dir, "restoring.marker"))
		return err == nil
	})
	killGroup(child)

	if b, _ := os.ReadFile(bTxt); string(b) != "beta\n" {
		t.Fatalf("life/b.txt after the kill: %q", b)
	}
	var st stateFile
	if b, err := os.ReadFile(filepath.Join(dir, "work", ".playground", "state.json")); err != nil || json.Unmarshal(b, &st) != nil {
		t.Fatalf("state.json after the kill: %v %s", err, b)
	}
	if st.Active == nil || st.Active.StepID != "write" {
		t.Fatalf("state.json active %+v", st.Active)
	}

	ts, _ = startKillChild(t, dir, gwSrv.URL, false)
	waitStep(t, ts, "write")
	var rr protocol.RestoreResult
	if code := ts.rpc("restore", protocol.RestoreRequest{}, &rr); code != 200 {
		t.Fatalf("Reset after the restart: %d", code)
	}
	if b, _ := os.ReadFile(bTxt); string(b) != "beta\n" {
		t.Fatalf("life/b.txt after Reset: %q", b)
	}
	playKeys(t, ts, demoOpen+" "+demoWrite)
	waitStep(t, ts, "split")
}

func playKeys(t *testing.T, ts *testServer, demo string) {
	t.Helper()
	err := protocol.PlayDemo(context.Background(), demo, 10, func(_ context.Context, req protocol.KeysRequest) error {
		if code := ts.rpc("keys", req, nil); code != 200 {
			return fmt.Errorf("keys %+v: %d", req, code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitStep(t *testing.T, ts *testServer, stepID string) {
	t.Helper()
	waitFor(t, "step "+stepID, func() bool {
		var st protocol.StatusResult
		return ts.rpc("status", nil, &st) == 200 && st.Step != nil && st.Step.StepID == stepID
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
