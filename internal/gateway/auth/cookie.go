package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Cookie errors.
var (
	ErrBadCookie     = errors.New("auth: malformed session cookie")
	ErrBadSignature  = errors.New("auth: bad session cookie signature")
	ErrCookieExpired = errors.New("auth: session cookie expired")
)

// Signer signs and verifies pg_session cookie values (backend.md §2.6):
// "<userID>.<expiry>.<signature>" where userID is base64url (so ids with
// dots, such as OIDC issuer-plus-subject ids, are safe), expiry is Unix
// seconds and signature is base64url HMAC-SHA256 over the first two parts.
type Signer struct{ key []byte }

// NewSigner returns a signer for key (at least 16 bytes).
func NewSigner(key []byte) (*Signer, error) {
	if len(key) < 16 {
		return nil, errors.New("auth: cookie secret shorter than 16 bytes")
	}
	return &Signer{key: append([]byte(nil), key...)}, nil
}

var b64 = base64.RawURLEncoding

func (s *Signer) mac(payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// Sign returns the cookie value for userID valid until expiry.
func (s *Signer) Sign(userID string, expiry time.Time) string {
	payload := b64.EncodeToString([]byte(userID)) + "." + strconv.FormatInt(expiry.Unix(), 10)
	return payload + "." + b64.EncodeToString(s.mac(payload))
}

// Verify checks a cookie value and returns the user id and expiry.
func (s *Signer) Verify(value string, now time.Time) (string, time.Time, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", time.Time{}, ErrBadCookie
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return "", time.Time{}, ErrBadCookie
	}
	if !hmac.Equal(sig, s.mac(parts[0]+"."+parts[1])) {
		return "", time.Time{}, ErrBadSignature
	}
	uid, err := b64.DecodeString(parts[0])
	if err != nil || len(uid) == 0 {
		return "", time.Time{}, ErrBadCookie
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", time.Time{}, ErrBadCookie
	}
	expiry := time.Unix(exp, 0)
	if !now.Before(expiry) {
		return "", time.Time{}, ErrCookieExpired
	}
	return string(uid), expiry, nil
}

// LoadOrCreateSecret reads the HMAC key from path, or generates 32 random
// bytes, writes them hex-encoded with mode 0600 and returns them
// (auth.cookieSecretFile, "generated on first start if missing"). The
// file's trimmed content is the key.
func LoadOrCreateSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key := bytes.TrimSpace(data)
		if len(key) < 16 {
			return nil, fmt.Errorf("auth: %s: cookie secret shorter than 16 bytes", path)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("auth: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	key := []byte(hex.EncodeToString(raw))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Lost a race with another process: use its secret.
			return LoadOrCreateSecret(path)
		}
		return nil, fmt.Errorf("auth: %w", err)
	}
	if _, err := f.Write(append(key, '\n')); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return key, nil
}
