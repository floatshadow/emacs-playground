package sshbox

import (
	"crypto/ed25519"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestKeyRoundTrip(t *testing.T) {
	priv, pub, err := protocol.GenerateBoxKey("playground-test")
	if err != nil {
		t.Fatal(err)
	}
	k, err := protocol.ParseBoxKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if got := protocol.AuthorizedKey(k.Public().(ed25519.PublicKey), "playground-test"); got != pub {
		t.Fatalf("public key %q, want %q", got, pub)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5") {
		t.Fatalf("unexpected authorized key %q", pub)
	}
	if _, err := protocol.ParseBoxKey([]byte("garbage")); err == nil {
		t.Fatal("garbage parsed")
	}
	if _, err := protocol.ParseBoxKey(priv[:len(priv)/2]); err == nil {
		t.Fatal("truncated key parsed")
	}
}

// TestKeyAcceptedBySSHKeygen checks the format against OpenSSH itself when
// ssh-keygen is installed: it must derive the same public key.
func TestKeyAcceptedBySSHKeygen(t *testing.T) {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	priv, pub, err := protocol.GenerateBoxKey("c")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "k")
	if err := os.WriteFile(p, priv, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-y", "-f", p).Output()
	if err != nil {
		t.Fatalf("ssh-keygen -y: %v", err)
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || f[0]+" "+f[1] != strings.Join(strings.Fields(pub)[:2], " ") {
		t.Fatalf("ssh-keygen derived %q, want %q", out, pub)
	}
	// And a key ssh-keygen wrote parses.
	p2 := filepath.Join(t.TempDir(), "k2")
	if err := exec.Command(bin, "-q", "-t", "ed25519", "-N", "", "-C", "x", "-f", p2).Run(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p2)
	k, err := protocol.ParseBoxKey(b)
	if err != nil {
		t.Fatal(err)
	}
	wantPub, _ := os.ReadFile(p2 + ".pub")
	if got := protocol.AuthorizedKey(k.Public().(ed25519.PublicKey), "x"); got != strings.TrimSpace(string(wantPub)) {
		t.Fatalf("parsed %q, want %q", got, wantPub)
	}
}

func TestSetup(t *testing.T) {
	dir := t.TempDir()
	c := Config{KeyDir: filepath.Join(dir, "keys"), SSHDir: filepath.Join(dir, "home", ".ssh"), Host: "box", Comment: "playground-local"}
	res, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Generated || !res.ConfigWritten {
		t.Fatalf("result %+v", res)
	}
	auth, _ := os.ReadFile(filepath.Join(c.KeyDir, AuthorizedKeysFile))
	if strings.TrimSpace(string(auth)) != res.PublicKey {
		t.Fatalf("authorized_keys %q", auth)
	}
	st, _ := os.Stat(filepath.Join(c.KeyDir, KeyFile))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", st.Mode())
	}
	cfg, _ := os.ReadFile(filepath.Join(c.SSHDir, "config"))
	for _, want := range []string{"Host box\n", "HostName box\n", "Port 22\n", "User deploy\n", "IdentityFile " + filepath.Join(c.KeyDir, KeyFile) + "\n", "BatchMode yes", "StrictHostKeyChecking no"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}

	// A second start keeps the pair.
	res2, err := Setup(c)
	if err != nil || res2.Generated || res2.PublicKey != res.PublicKey {
		t.Fatalf("second setup %+v, %v", res2, err)
	}

	// A key from the gateway replaces it.
	priv, pub, _ := protocol.GenerateBoxKey("playground-local")
	c.PrivateKey = string(priv)
	res3, err := Setup(c)
	if err != nil || res3.PublicKey != pub {
		t.Fatalf("setup with key %+v, %v", res3, err)
	}
	auth, _ = os.ReadFile(filepath.Join(c.KeyDir, AuthorizedKeysFile))
	if strings.TrimSpace(string(auth)) != pub {
		t.Fatalf("authorized_keys not updated")
	}

	// The learner's own config is left alone.
	own := []byte("Host *\n  ForwardAgent no\n")
	_ = os.WriteFile(filepath.Join(c.SSHDir, "config"), own, 0o600)
	res4, err := Setup(c)
	if err != nil || res4.ConfigWritten {
		t.Fatalf("setup over the learner's config %+v, %v", res4, err)
	}
	if b, _ := os.ReadFile(filepath.Join(c.SSHDir, "config")); string(b) != string(own) {
		t.Fatal("learner config overwritten")
	}

	if _, err := Setup(Config{KeyDir: c.KeyDir}); err == nil {
		t.Fatal("setup without a host succeeded")
	}
	if _, err := Setup(Config{KeyDir: c.KeyDir, Host: "box", PrivateKey: "nope"}); err == nil {
		t.Fatal("bad PG_BOX_KEY accepted")
	}
}
