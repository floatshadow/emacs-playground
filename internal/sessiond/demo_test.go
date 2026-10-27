package sessiond

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// TestDemoSkipAndDropAll covers rpc demo, rpc skip and drop-step {} in fake
// mode (api.md §3.4, §4.1).
func TestDemoSkipAndDropAll(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	ws := filepath.Join(base, "work")
	run, err := os.MkdirTemp("", "pgrun")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	_ = os.MkdirAll(filepath.Join(base, "pristine"), 0o755)
	gw := &fakeGateway{tarball: makeTarball(t, map[string]string{"README.md": "# hello\n"})}
	gwSrv := httptest.NewServer(gw.handler(t))
	defer gwSrv.Close()
	ts := startServer(t, ws, run, gw, gwSrv.URL)
	defer ts.stop()
	es := ts.events("")
	_ = os.WriteFile(filepath.Join(ws, "emacs.d", "modules.el"), []byte("(setq playground-modules '(base agent editing))\n"), 0o644)

	lesson := "00-orientation/02-first-steps"
	var sr protocol.ScenarioResult
	if code := ts.rpc("scenario", protocol.ScenarioRequest{LessonID: lesson}, &sr); code != 200 {
		t.Fatalf("scenario %d", code)
	}
	es.wait("step:open:loaded")

	// demo: only for the active step, and a valid phase.
	if code := ts.rpc("demo", protocol.DemoRequest{StepID: "write", Phase: protocol.DemoStart}, nil); code != 409 {
		t.Fatalf("demo of another step: %d", code)
	}
	if code := ts.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: "middle"}, nil); code != 400 {
		t.Fatalf("bad phase: %d", code)
	}
	var dr protocol.DemoResult
	if code := ts.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, &dr); code != 200 || dr.Restored {
		t.Fatalf("demo start %d %+v", code, dr)
	}
	ts.srv.OnEvent(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file", Frame: protocol.FrameTerminal})
	es.wait("step:open:demonstrated")
	if code := ts.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoEnd}, &dr); code != 200 {
		t.Fatalf("demo end %d", code)
	}
	var st protocol.StatusResult
	if ts.rpc("status", nil, &st) != 200 || st.Step == nil || st.Step.StepID != "open" {
		t.Fatalf("the demonstrated step must stay active: %+v", st.Step)
	}
	// Watch it again after commands: the checkpoint is restored first.
	if code := ts.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, &dr); code != 200 || !dr.Restored {
		t.Fatalf("second demo start %d %+v", code, dr)
	}
	es.wait("step:open:reset")
	// Do it yourself: restore {} clears the latch; the learner's completion
	// is done and advances.
	if ts.rpc("restore", protocol.RestoreRequest{}, nil) != 200 {
		t.Fatal("restore")
	}
	ts.srv.OnEvent(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file", Frame: protocol.FrameTerminal})
	es.wait("step:open:complete")
	es.wait("step:write:loaded")

	waitProgress(t, gw, "open:demonstrated", "open:done")

	// drop-step {}: the active step goes, with a dropped event, and all
	// saved progress with it.
	if code := ts.rpc("drop-step", protocol.DropStepRequest{}, nil); code != 200 {
		t.Fatalf("drop-step {} %d", code)
	}
	es.wait("step:write:dropped")
	if ts.rpc("check", nil, nil) != 409 {
		t.Fatal("no step is active after drop-step {}")
	}
	if code := ts.rpc("drop-step", protocol.DropStepRequest{LessonID: "../x"}, nil); code != 400 {
		t.Fatalf("drop-step with a bad id: %d", code)
	}

	// The lesson starts over at its first step; skip it: recorded skipped,
	// the next step loads.
	if code := ts.rpc("scenario", protocol.ScenarioRequest{LessonID: lesson}, &sr); code != 200 || sr.Step.StepID != "open" {
		t.Fatalf("scenario after drop %d %+v", code, sr)
	}
	if code := ts.rpc("skip", protocol.SkipRequest{StepID: "write"}, nil); code != 409 {
		t.Fatalf("skip of another step: %d", code)
	}
	var sk protocol.SkipResult
	if code := ts.rpc("skip", protocol.SkipRequest{StepID: "open"}, &sk); code != 200 || sk.Next == nil || *sk.Next != "write" || sk.DemoCompleted {
		t.Fatalf("skip %d %+v", code, sk)
	}
	es.wait("step:open:skipped")
	waitProgress(t, gw, "open:skipped")
	if ts.rpc("status", nil, &st) != 200 || st.Step == nil || st.Step.StepID != "write" {
		t.Fatalf("after skip: %+v", st.Step)
	}
}

func waitProgress(t *testing.T, gw *fakeGateway, want ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var joined string
	for time.Now().Before(deadline) {
		gw.mu.Lock()
		var summary []string
		for _, p := range gw.progress {
			summary = append(summary, p.StepID+":"+string(p.Status))
		}
		gw.mu.Unlock()
		joined = strings.Join(summary, " ")
		ok := true
		for _, w := range want {
			if !strings.Contains(joined, w) {
				ok = false
			}
		}
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("progress %s lacks one of %v", joined, want)
}
