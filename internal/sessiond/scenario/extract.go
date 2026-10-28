package scenario

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/klauspost/compress/zstd"
)

// Extraction limits.
const (
	MaxFixtureFile  = 100 << 20
	MaxFixtureTotal = 1 << 30
)

// ExtractResult counts what an extraction did.
type ExtractResult struct {
	Written int
	Skipped int
	Dirs    int
	// Removed counts files and directories deleted because the fixture does
	// not contain them (overwrite into a non-empty dest only).
	Removed int
}

// KeepOnReset are directory names a reset never removes: build output
// costs a rebuild, and .git is the learner's own history (backend.md §3.9).
var KeepOnReset = map[string]bool{"target": true, "_build": true, ".venv": true, "node_modules": true, ".git": true}

// Extract unpacks a zstd-compressed tar stream into dest (a slash path
// relative to root). Existing files are kept unless overwrite. Only regular
// files and directories are allowed; absolute and ".." names are rejected
// (the bundle contract in protocol/curriculum.go). Writing through os.Root
// means a symlink the learner created cannot redirect files outside the
// workspace.
//
// With overwrite and a non-empty dest (setup.reset), dest is made to match
// the fixture: files and directories the tarball does not contain are
// removed afterwards (except the KeepOnReset directories), so a lesson
// replayed after a learner moved, renamed or created files starts from the
// same tree again.
func Extract(r io.Reader, root *os.Root, dest string, overwrite bool) (ExtractResult, error) {
	seen := map[string]bool{}
	res, err := extract(r, root, dest, overwrite, seen)
	if err != nil || !overwrite {
		return res, err
	}
	dest = strings.Trim(path.Clean("/"+dest), "/")
	if dest == "" {
		return res, nil
	}
	n, err := prune(root, dest, seen)
	res.Removed = n
	return res, err
}

// prune removes what is under dest but not in seen (paths relative to
// dest), keeping KeepOnReset directories.
func prune(root *os.Root, dest string, seen map[string]bool) (int, error) {
	removed := 0
	var walk func(rel string) error
	walk = func(rel string) error {
		dir := dest
		if rel != "" {
			dir = dest + "/" + rel
		}
		f, err := root.Open(dir)
		if err != nil {
			return nil
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return nil
		}
		for _, e := range entries {
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			if e.IsDir() && KeepOnReset[e.Name()] {
				continue
			}
			if !seen[child] {
				// EBUSY: on NFS a file a process still holds open becomes
				// .nfsXXXX and cannot be removed until it is closed (an
				// epdfinfo serving a removed PDF); everything else is gone.
				if err := root.RemoveAll(dest + "/" + child); err != nil && !errors.Is(err, syscall.EBUSY) {
					return fmt.Errorf("fixtures: removing %s: %w", child, err)
				}
				removed++
				continue
			}
			if e.IsDir() {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return removed, walk("")
}

func extract(r io.Reader, root *os.Root, dest string, overwrite bool, seen map[string]bool) (ExtractResult, error) {
	var res ExtractResult
	zr, err := zstd.NewReader(r, zstd.WithDecoderMaxMemory(256<<20))
	if err != nil {
		return res, fmt.Errorf("fixtures: %w", err)
	}
	defer zr.Close()
	dest = strings.Trim(path.Clean("/"+dest), "/")
	if dest != "" {
		if err := root.MkdirAll(dest, 0o755); err != nil {
			return res, fmt.Errorf("fixtures: creating %s: %w", dest, err)
		}
	}
	tr := tar.NewReader(zr)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return res, fmt.Errorf("fixtures: reading tarball: %w", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "" || name == "." {
			continue
		}
		clean := path.Clean(name)
		if path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || !fs.ValidPath(clean) {
			return res, fmt.Errorf("fixtures: unsafe path %q", hdr.Name)
		}
		target := clean
		if dest != "" {
			target = dest + "/" + clean
		}
		for p := clean; p != "." && p != "/"; p = path.Dir(p) {
			seen[p] = true
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(target, 0o755); err != nil {
				return res, fmt.Errorf("fixtures: %w", err)
			}
			res.Dirs++
		case tar.TypeReg:
			if hdr.Size > MaxFixtureFile {
				return res, fmt.Errorf("fixtures: %s is larger than %d bytes", clean, MaxFixtureFile)
			}
			total += hdr.Size
			if total > MaxFixtureTotal {
				return res, fmt.Errorf("fixtures: tarball larger than %d bytes", MaxFixtureTotal)
			}
			if st, err := root.Lstat(target); err == nil {
				if st.IsDir() {
					return res, fmt.Errorf("fixtures: %s is a directory in the workspace", target)
				}
				if !overwrite {
					res.Skipped++
					continue
				}
				if st.Mode()&fs.ModeSymlink != 0 {
					// Replace the link itself, never write through it.
					if err := root.Remove(target); err != nil {
						return res, fmt.Errorf("fixtures: %w", err)
					}
				}
			}
			if dir := path.Dir(target); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return res, fmt.Errorf("fixtures: %w", err)
				}
			}
			mode := fs.FileMode(hdr.Mode).Perm() | 0o600
			f, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return res, fmt.Errorf("fixtures: %w", err)
			}
			_, err = io.Copy(f, io.LimitReader(tr, hdr.Size))
			cerr := f.Close()
			if err == nil {
				err = cerr
			}
			if err != nil {
				return res, fmt.Errorf("fixtures: writing %s: %w", target, err)
			}
			_ = root.Chmod(target, mode)
			res.Written++
		default:
			// Symlinks, devices and the like are not part of the contract.
			continue
		}
	}
}
