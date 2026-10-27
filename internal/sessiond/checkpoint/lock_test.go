package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// lockRunner fails like git while the lock file exists and records the
// context each command ran with.
type lockRunner struct {
	lock   string
	calls  int
	ctxErr []error
}

func (r *lockRunner) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	r.calls++
	r.ctxErr = append(r.ctxErr, ctx.Err())
	if _, err := os.Stat(r.lock); err == nil {
		return nil, fmt.Errorf("git %v: exit status 128: fatal: Unable to create '%s': File exists.", args, r.lock)
	}
	return []byte("ok\n"), nil
}

func TestStaleIndexLockIsRemoved(t *testing.T) {
	ws := t.TempDir()
	lock := filepath.Join(ws, ".git", "index.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	r := &lockRunner{lock: lock}
	m := &Manager{Workspace: ws, Git: r, Now: time.Now}
	out, err := m.git(context.Background(), nil, "restore", "--source=x", "--", ".")
	if err != nil || out != "ok" {
		t.Fatalf("git = %q, %v; want ok after removing the stale lock", out, err)
	}
	if r.calls != 2 {
		t.Fatalf("calls = %d, want 2 (the failure, then the retry)", r.calls)
	}
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock still there: %v", err)
	}
}

func TestFreshIndexLockIsKept(t *testing.T) {
	ws := t.TempDir()
	lock := filepath.Join(ws, ".git", "index.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &lockRunner{lock: lock}
	m := &Manager{Workspace: ws, Git: r, Now: time.Now}
	if _, err := m.git(context.Background(), nil, "add", "-A"); err == nil {
		t.Fatal("a lock another git process holds right now must not be removed")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock removed: %v", err)
	}
}

func TestGitOutlivesTheRequest(t *testing.T) {
	ws := t.TempDir()
	r := &lockRunner{lock: filepath.Join(ws, "none.lock")}
	m := &Manager{Workspace: ws, Git: r, Now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.git(ctx, nil, "restore", "--", "."); err != nil {
		t.Fatal(err)
	}
	if r.ctxErr[0] != nil {
		t.Fatalf("git ran with a cancelled context (%v): a request that ends must not kill git holding the index lock", r.ctxErr[0])
	}
}
