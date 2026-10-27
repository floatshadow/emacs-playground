package preview

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// SandboxCSP is sent with HTML and other active documents (backend.md
// §3.10).
const SandboxCSP = "sandbox allow-scripts allow-same-origin"

// Hidden lists top-level workspace entries that are never served or listed.
var Hidden = map[string]bool{".playground": true}

// Server serves GET /preview/{path...}.
type Server struct {
	root *os.Root
}

// New opens the workspace root. Close it with Close.
func New(workspace string) (*Server, error) {
	r, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	return &Server{root: r}, nil
}

// Close releases the root.
func (s *Server) Close() error { return s.root.Close() }

// Root returns the workspace root (shared with the step engine's file
// conditions).
func (s *Server) Root() *os.Root { return s.root }

// cleanPath turns the URL path below /preview/ into a root-relative name.
// It reports whether the request asked for a directory (trailing slash) and
// rejects names that are not local.
func cleanPath(p string) (name string, dir bool, ok bool) {
	dir = p == "" || strings.HasSuffix(p, "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return ".", true, true
	}
	if strings.Contains(p, "\\") || strings.ContainsRune(p, 0) {
		return "", dir, false
	}
	clean := path.Clean(p)
	if clean != p || !fs.ValidPath(clean) {
		return "", dir, false
	}
	first := clean
	if i := strings.IndexByte(clean, '/'); i >= 0 {
		first = clean[:i]
	}
	if Hidden[first] {
		return "", dir, false
	}
	return clean, dir, true
}

// ServeHTTP serves the file or listing named by the path after /preview/.
// The mux pattern is "GET /preview/{path...}".
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("path")
	if rest == "" {
		rest = strings.TrimPrefix(r.URL.Path, protocol.PathPreview)
		if r.URL.Path == strings.TrimSuffix(protocol.PathPreview, "/") {
			rest = ""
		}
	}
	if strings.HasSuffix(r.URL.Path, "/") && !strings.HasSuffix(rest, "/") && rest != "" {
		rest += "/"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name, wantDir, ok := cleanPath(rest)
	if !ok {
		httpx.WriteError(w, r, httpx.NotFound("no such file in the workspace"))
		return
	}
	f, err := s.root.Open(name)
	if err != nil {
		httpx.WriteError(w, r, notFound(err))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		httpx.WriteError(w, r, notFound(err))
		return
	}
	if st.IsDir() {
		s.list(w, r, f, name)
		return
	}
	if wantDir {
		httpx.WriteError(w, r, httpx.NotFound("not a directory"))
		return
	}
	if !st.Mode().IsRegular() {
		httpx.WriteError(w, r, httpx.NotFound("not a regular file"))
		return
	}
	ctype := contentType(name)
	if ctype == "" {
		// No extension: sniff like ServeContent would, so the CSP decision
		// sees the same type the browser gets.
		var buf [512]byte
		n, _ := io.ReadFull(f, buf[:])
		ctype = http.DetectContentType(buf[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			httpx.WriteError(w, r, httpx.Internal("could not read the file").Wrap(err))
			return
		}
	}
	w.Header().Set("Content-Type", ctype)
	if isActive(name, ctype) {
		w.Header().Set("Content-Security-Policy", SandboxCSP)
	}
	http.ServeContent(w, r, path.Base(name), st.ModTime(), f)
}

func notFound(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return httpx.NotFound("no such file in the workspace")
	}
	// Escapes through symlinks or "..", permission errors: hide details.
	return httpx.NotFound("no such file in the workspace").Wrap(err)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, f *os.File, name string) {
	ents, err := f.ReadDir(-1)
	if err != nil {
		httpx.WriteError(w, r, httpx.Internal("could not list the directory").Wrap(err))
		return
	}
	out := protocol.PreviewListing{Entries: []protocol.PreviewEntry{}}
	if name != "." {
		out.Path = name
	}
	for _, e := range ents {
		if name == "." && Hidden[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		isDir := e.IsDir()
		if info.Mode()&fs.ModeSymlink != 0 {
			// Report the target type when it stays inside the root.
			if st, err := s.root.Stat(path.Join(name, e.Name())); err == nil {
				isDir = st.IsDir()
				info = st
			}
		}
		entry := protocol.PreviewEntry{Name: e.Name(), Dir: isDir, Mtime: info.ModTime().UnixMilli()}
		if !isDir {
			entry.Size = info.Size()
		}
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return a.Name < b.Name
	})
	httpx.WriteJSON(w, http.StatusOK, out)
}

// contentType picks a type by extension; text types get a UTF-8 charset.
func contentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".typ", ".rs", ".ml", ".mli", ".py", ".el", ".org", ".tex", ".bib", ".toml", ".yaml", ".yml", ".sh", ".txt", ".log", ".csv", ".dune", ".opam":
		return "text/plain; charset=utf-8"
	case "":
		return ""
	}
	return mime.TypeByExtension(ext)
}

// isActive reports content a browser might execute when the preview iframe
// navigates to it directly.
func isActive(name, ctype string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".html", ".htm", ".xhtml", ".svg", ".xml", ".xsl", ".xslt":
		return true
	}
	return strings.HasPrefix(ctype, "text/html") || strings.Contains(ctype, "xml")
}
