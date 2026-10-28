// Package sshbox gives the session its remote box (deployment.md §6): an
// ed25519 key pair under /work/.playground/ssh and a "box" Host alias in
// ~/.ssh/config, so `ssh box` and TRAMP's /ssh:box: log in without a
// prompt.
//
// Locally sessiond generates the pair on first start and writes
// authorized_keys next to it; the shared box mounts that directory
// read-only and reads authorized_keys at every login. In server mode the
// gateway generates the pair, gives the public half to the learner's box
// and the private half to sessiond (PG_BOX_KEY), which writes it here.
//
// The box's host key changes whenever its container is re-created, so the
// alias neither checks nor records host keys (the box is reachable only on
// the session's own network).
package sshbox

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Files under the key directory.
const (
	KeyFile            = "id_ed25519"
	PubFile            = "id_ed25519.pub"
	AuthorizedKeysFile = "authorized_keys"
	// Marker starts the ssh client configuration sessiond writes; a file
	// without it is the learner's and is left alone.
	Marker = "# emacs-playground: written by sessiond"
)

// Alias is the Host name of the box.
const Alias = "box"

// Config says where and how to set up the box alias.
type Config struct {
	// KeyDir holds the key pair and authorized_keys
	// (/work/.playground/ssh).
	KeyDir string
	// SSHDir is the ssh client directory (~/.ssh).
	SSHDir string
	// Host, Port and User are the box's address and login.
	Host string
	Port int
	User string
	// PrivateKey, when set, is the pair to use (OpenSSH format); else a
	// pair in KeyDir is kept or generated.
	PrivateKey string
	// Comment is the public key's comment ("playground-<session id>").
	Comment string
}

// Result says what Setup did.
type Result struct {
	// PublicKey is the authorized_keys line.
	PublicKey string
	// Generated is true when a new pair was made.
	Generated bool
	// ConfigWritten is false when ~/.ssh/config belongs to the learner.
	ConfigWritten bool
}

// Setup makes sure the key pair, authorized_keys and the alias exist.
func Setup(c Config) (Result, error) {
	var res Result
	if c.Host == "" {
		return res, errors.New("sshbox: no box host")
	}
	if c.Port <= 0 {
		c.Port = 22
	}
	if c.User == "" {
		c.User = "deploy"
	}
	if err := os.MkdirAll(c.KeyDir, 0o700); err != nil {
		return res, err
	}
	keyPath := filepath.Join(c.KeyDir, KeyFile)
	var priv ed25519.PrivateKey
	switch {
	case c.PrivateKey != "":
		k, err := protocol.ParseBoxKey([]byte(c.PrivateKey))
		if err != nil {
			return res, fmt.Errorf("sshbox: PG_BOX_KEY: %w", err)
		}
		priv = k
	default:
		if b, err := os.ReadFile(keyPath); err == nil {
			if k, perr := protocol.ParseBoxKey(b); perr == nil {
				priv = k
			}
		}
		if priv == nil {
			_, k, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return res, err
			}
			priv = k
			res.Generated = true
		}
	}
	pemBytes, err := protocol.MarshalBoxKey(priv, c.Comment)
	if err != nil {
		return res, err
	}
	res.PublicKey = protocol.AuthorizedKey(priv.Public().(ed25519.PublicKey), c.Comment)
	if res.Generated || c.PrivateKey != "" {
		if err := writeIfChanged(keyPath, pemBytes, 0o600); err != nil {
			return res, err
		}
	}
	// ssh refuses a private key others can read.
	_ = os.Chmod(keyPath, 0o600)
	if err := writeIfChanged(filepath.Join(c.KeyDir, PubFile), []byte(res.PublicKey+"\n"), 0o644); err != nil {
		return res, err
	}
	if err := writeIfChanged(filepath.Join(c.KeyDir, AuthorizedKeysFile), []byte(res.PublicKey+"\n"), 0o644); err != nil {
		return res, err
	}
	written, err := writeClientConfig(c, keyPath)
	res.ConfigWritten = written
	return res, err
}

// ClientConfig is the ssh configuration of the alias.
func ClientConfig(c Config, keyPath string) string {
	var b strings.Builder
	b.WriteString(Marker + " (deployment.md §6); rewritten at every start.\n")
	b.WriteString("Host " + Alias + "\n")
	b.WriteString("  HostName " + c.Host + "\n")
	b.WriteString("  Port " + strconv.Itoa(c.Port) + "\n")
	b.WriteString("  User " + c.User + "\n")
	b.WriteString("  IdentityFile " + keyPath + "\n")
	b.WriteString("  IdentitiesOnly yes\n")
	b.WriteString("  BatchMode yes\n")
	b.WriteString("  StrictHostKeyChecking no\n")
	b.WriteString("  UserKnownHostsFile /dev/null\n")
	b.WriteString("  LogLevel ERROR\n")
	b.WriteString("  ServerAliveInterval 30\n")
	return b.String()
}

func writeClientConfig(c Config, keyPath string) (bool, error) {
	if c.SSHDir == "" {
		return false, nil
	}
	if err := os.MkdirAll(c.SSHDir, 0o700); err != nil {
		return false, err
	}
	p := filepath.Join(c.SSHDir, "config")
	if cur, err := os.ReadFile(p); err == nil && !bytes.HasPrefix(cur, []byte(Marker)) {
		return false, nil
	}
	return true, writeIfChanged(p, []byte(ClientConfig(c, keyPath)), 0o600)
}

func writeIfChanged(p string, data []byte, mode os.FileMode) error {
	if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
