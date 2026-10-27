package auth

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/store"
)

// MinCodeLength is the shortest accepted invite code.
const MinCodeLength = 8

// InviteSpec is one line of the invites file (deploy/invites.example.txt):
//
//	<code> [label] [uses=N] [until=YYYY-MM-DD]
//
// Lines starting with # and blank lines are ignored. The label is every
// token without "=" after the code, joined with spaces.
type InviteSpec struct {
	Code    string
	Label   string
	MaxUses int
	// Until is the last valid day (UTC); zero means no expiry.
	Until time.Time
}

// ExpiresAt returns the expiry in ms: the start of the day after Until, or
// 0.
func (s InviteSpec) ExpiresAt() int64 {
	if s.Until.IsZero() {
		return 0
	}
	return s.Until.AddDate(0, 0, 1).UnixMilli()
}

// HashCode returns the hex SHA-256 of an invite code; only hashes are
// stored ("codes are hashed in the database on first load").
func HashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

// ParseInvites parses an invites file.
func ParseInvites(r io.Reader) ([]InviteSpec, error) {
	var out []InviteSpec
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		spec := InviteSpec{Code: fields[0]}
		if strings.Contains(spec.Code, "=") {
			return nil, fmt.Errorf("invites line %d: the first field must be the code", n)
		}
		if len(spec.Code) < MinCodeLength {
			return nil, fmt.Errorf("invites line %d: code shorter than %d characters", n, MinCodeLength)
		}
		if prev, dup := seen[spec.Code]; dup {
			return nil, fmt.Errorf("invites line %d: code repeated from line %d", n, prev)
		}
		seen[spec.Code] = n
		var label []string
		for _, f := range fields[1:] {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				label = append(label, f)
				continue
			}
			switch k {
			case "uses":
				u, err := strconv.Atoi(v)
				if err != nil || u < 1 {
					return nil, fmt.Errorf("invites line %d: uses=%s is not a positive number", n, v)
				}
				spec.MaxUses = u
			case "until":
				d, err := time.Parse("2006-01-02", v)
				if err != nil {
					return nil, fmt.Errorf("invites line %d: until=%s is not YYYY-MM-DD", n, v)
				}
				spec.Until = d
			default:
				return nil, fmt.Errorf("invites line %d: unknown option %q (uses=N, until=YYYY-MM-DD)", n, k)
			}
		}
		spec.Label = strings.Join(label, " ")
		out = append(out, spec)
	}
	return out, sc.Err()
}

// LoadInvites reads an invites file into store rows (hashed codes).
func LoadInvites(path string) ([]store.Invite, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("auth: invites: %w", err)
	}
	defer f.Close()
	specs, err := ParseInvites(f)
	if err != nil {
		return nil, fmt.Errorf("auth: %s: %w", path, err)
	}
	out := make([]store.Invite, 0, len(specs))
	for _, s := range specs {
		out = append(out, store.Invite{CodeHash: HashCode(s.Code), Label: s.Label, MaxUses: s.MaxUses, ExpiresAt: s.ExpiresAt()})
	}
	return out, nil
}
