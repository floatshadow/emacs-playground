package curriculum

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

// ErrFormat is returned when the bundle's format differs from the
// gateway's.
var ErrFormat = errors.New("curriculum: bundle format mismatch")

var lessonIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`)

// Options configure Open.
type Options struct {
	Dir           string
	DefaultLocale string
	// AutoReload re-reads the bundle when curriculum.json changes (checked
	// at most every two seconds; author mode).
	AutoReload bool
	Logger     *slog.Logger
}

// Server serves one bundle directory.
type Server struct {
	o Options

	mu       sync.RWMutex
	root     *os.Root
	loaded   bool
	loadErr  error
	version  string
	format   int
	locales  []string            // index locales besides the default
	lessons  map[string][]string // ready lesson id → step ids
	modTime  time.Time
	checked  time.Time
	etagMu   sync.Mutex
	etags    map[string]etagEntry
	indexDoc *protocol.CurriculumIndex
}

type etagEntry struct {
	size  int64
	mtime time.Time
	etag  string
}

// Open loads the bundle. A missing directory or curriculum.json is not an
// error (the server answers 503 until Reload finds it); a format mismatch
// or a corrupt index is.
func Open(o Options) (*Server, error) {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.DefaultLocale == "" {
		o.DefaultLocale = protocol.DefaultLocale
	}
	s := &Server{o: o, etags: map[string]etagEntry{}}
	err := s.Reload()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		o.Logger.Warn("curriculum: no bundle yet; curriculum endpoints answer 503 until it is built",
			"dir", o.Dir, "hint", "playctl curriculum build curriculum -o "+o.Dir)
	}
	return s, nil
}

// Reload re-reads curriculum.json and the lesson scenarios.
func (s *Server) Reload() error {
	root, err := os.OpenRoot(s.o.Dir)
	if err != nil {
		s.setErr(err)
		return err
	}
	data, err := root.ReadFile(protocol.BundleIndexFile)
	if err != nil {
		root.Close()
		s.setErr(err)
		return err
	}
	st, _ := root.Stat(protocol.BundleIndexFile)
	var idx protocol.CurriculumIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		root.Close()
		err = fmt.Errorf("curriculum: %s: %w", protocol.BundleIndexFile, err)
		s.setErr(err)
		return err
	}
	if idx.Format != protocol.CurriculumFormat {
		root.Close()
		err := fmt.Errorf("%w: %s has format %d, this gateway serves format %d; rebuild it with this release's playctl",
			ErrFormat, filepath.Join(s.o.Dir, protocol.BundleIndexFile), idx.Format, protocol.CurriculumFormat)
		s.setErr(err)
		return err
	}
	lessons := map[string][]string{}
	ids := []string{}
	for _, p := range idx.Parts {
		for _, l := range p.Lessons {
			if l.Status == protocol.Ready {
				ids = append(ids, l.ID)
			}
		}
	}
	for _, p := range idx.Projects {
		if p.Status == protocol.Ready {
			ids = append(ids, p.ID)
		}
	}
	for _, id := range ids {
		if !lessonIDRe.MatchString(id) {
			continue
		}
		raw, err := root.ReadFile(protocol.LessonDir(id) + "/" + protocol.BundleScenario)
		if err != nil {
			s.o.Logger.Warn("curriculum: ready lesson without scenario.json", "lesson", id, "err", err)
			continue
		}
		var sc struct {
			Steps []struct {
				ID string `json:"id"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &sc); err != nil {
			s.o.Logger.Warn("curriculum: bad scenario.json", "lesson", id, "err", err)
			continue
		}
		steps := make([]string, 0, len(sc.Steps))
		for _, st := range sc.Steps {
			steps = append(steps, st.ID)
		}
		lessons[id] = steps
	}
	var locales []string
	if entries, err := fs.ReadDir(root.FS(), "."); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "curriculum.") && strings.HasSuffix(name, ".json") && name != protocol.BundleIndexFile {
				locales = append(locales, strings.TrimSuffix(strings.TrimPrefix(name, "curriculum."), ".json"))
			}
		}
	}
	sort.Strings(locales)

	s.mu.Lock()
	old := s.root
	s.root, s.loaded, s.loadErr = root, true, nil
	s.version, s.format, s.locales, s.lessons = idx.Version, idx.Format, locales, lessons
	s.indexDoc = &idx
	if st != nil {
		s.modTime = st.ModTime()
	}
	s.checked = time.Now()
	s.mu.Unlock()
	s.etagMu.Lock()
	s.etags = map[string]etagEntry{}
	s.etagMu.Unlock()
	if old != nil {
		// In-flight requests may still read through the old root.
		time.AfterFunc(time.Minute, func() { old.Close() })
	}
	s.o.Logger.Info("curriculum: loaded", "dir", s.o.Dir, "version", idx.Version, "lessons", len(lessons), "locales", append([]string{s.o.DefaultLocale}, locales...))
	return nil
}

func (s *Server) setErr(err error) {
	s.mu.Lock()
	s.loadErr = err
	s.checked = time.Now()
	s.mu.Unlock()
}

// Loaded reports whether a bundle is being served, and its version.
func (s *Server) Loaded() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded, s.version
}

// StepIDs returns the step ids of a ready lesson.
func (s *Server) StepIDs(lessonID string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	steps, ok := s.lessons[lessonID]
	return steps, ok
}

// Index returns the parsed default-locale index, or nil.
func (s *Server) Index() *protocol.CurriculumIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexDoc
}

// maybeReload reloads in author mode when curriculum.json changed.
func (s *Server) maybeReload() {
	if !s.o.AutoReload {
		return
	}
	s.mu.RLock()
	due := time.Since(s.checked) > 2*time.Second
	mod := s.modTime
	loaded := s.loaded
	s.mu.RUnlock()
	if !due {
		return
	}
	st, err := os.Stat(filepath.Join(s.o.Dir, protocol.BundleIndexFile))
	if err != nil {
		s.setErr(err)
		return
	}
	if !loaded || !st.ModTime().Equal(mod) {
		if err := s.Reload(); err != nil {
			s.o.Logger.Warn("curriculum: reload", "err", err)
		}
		return
	}
	s.mu.Lock()
	s.checked = time.Now()
	s.mu.Unlock()
}

// Routes registers the curriculum endpoints. protect wraps the learner
// endpoints (auth); internal wraps the sessiond endpoints (bearer token).
func (s *Server) Routes(mux *http.ServeMux, protect, internal func(http.Handler) http.Handler) {
	mux.Handle("GET "+protocol.APIPrefix+"/curriculum", protect(httpx.Handle(s.index)))
	mux.Handle("GET "+protocol.APIPrefix+"/curriculum/lessons/{lessonId...}", protect(httpx.Handle(s.lesson)))
	mux.Handle("GET "+protocol.APIPrefix+"/curriculum/assets/{path...}", protect(httpx.Handle(s.asset)))
	mux.Handle("GET "+protocol.InternalScenarioPath+"{lessonId...}", internal(httpx.Handle(s.scenario)))
	mux.Handle("GET "+protocol.InternalFixturesPath+"{lessonId...}", internal(httpx.Handle(s.fixtures)))
}

func (s *Server) rootOrErr() (*os.Root, error) {
	s.maybeReload()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		msg := "the curriculum bundle is not available"
		if s.loadErr != nil && !errors.Is(s.loadErr, fs.ErrNotExist) {
			msg += ": " + s.loadErr.Error()
		}
		return nil, &httpx.Error{Status: http.StatusServiceUnavailable, Code: protocol.ErrInternal, Message: msg}
	}
	return s.root, nil
}

// locale picks ?locale=, then Accept-Language, then the default.
func (s *Server) locale(r *http.Request, available func(loc string) bool) string {
	if q := r.URL.Query().Get("locale"); q != "" {
		if q == s.o.DefaultLocale || available(q) {
			return q
		}
		if base, _, ok := strings.Cut(q, "-"); ok && available(base) {
			return base
		}
	}
	for _, tag := range parseAcceptLanguage(r.Header.Get("Accept-Language")) {
		if tag == s.o.DefaultLocale || strings.HasPrefix(tag, s.o.DefaultLocale+"-") {
			return s.o.DefaultLocale
		}
		if available(tag) {
			return tag
		}
		if base, _, ok := strings.Cut(tag, "-"); ok && available(base) {
			return base
		}
		// "zh" asks for any Chinese variant we have.
		if !strings.Contains(tag, "-") {
			s.mu.RLock()
			for _, l := range s.locales {
				if strings.HasPrefix(l, tag+"-") && available(l) {
					s.mu.RUnlock()
					return l
				}
			}
			s.mu.RUnlock()
		}
	}
	return s.o.DefaultLocale
}

// parseAcceptLanguage returns tags by descending quality.
func parseAcceptLanguage(h string) []string {
	type tq struct {
		tag string
		q   float64
	}
	var out []tq
	for _, part := range strings.Split(h, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		out = append(out, tq{tag, q})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].q > out[j].q })
	tags := make([]string, len(out))
	for i, t := range out {
		tags[i] = t.tag
	}
	return tags
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) error {
	root, err := s.rootOrErr()
	if err != nil {
		return err
	}
	loc := s.locale(r, func(l string) bool {
		_, err := root.Stat(protocol.BundleIndexName(l))
		return err == nil
	})
	w.Header().Set("Content-Language", loc)
	w.Header().Add("Vary", "Accept-Language")
	return s.serveFile(w, r, root, protocol.BundleIndexName(loc), "application/json; charset=utf-8", "no-cache")
}

func (s *Server) lessonID(r *http.Request) (string, error) {
	id := r.PathValue("lessonId")
	if !lessonIDRe.MatchString(id) {
		return "", httpx.NotFound("no such lesson")
	}
	return id, nil
}

func (s *Server) lesson(w http.ResponseWriter, r *http.Request) error {
	root, err := s.rootOrErr()
	if err != nil {
		return err
	}
	id, err := s.lessonID(r)
	if err != nil {
		return err
	}
	dir := protocol.LessonDir(id)
	loc := s.locale(r, func(l string) bool {
		_, err := root.Stat(dir + "/" + protocol.BundleLessonName(l))
		return err == nil
	})
	w.Header().Set("Content-Language", loc)
	w.Header().Add("Vary", "Accept-Language")
	return s.serveFile(w, r, root, dir+"/"+protocol.BundleLessonName(loc), "application/json; charset=utf-8", "no-cache")
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request) error {
	root, err := s.rootOrErr()
	if err != nil {
		return err
	}
	p := r.PathValue("path")
	parts := strings.SplitN(p, "/", 3)
	if len(parts) != 3 || !lessonIDRe.MatchString(parts[0]+"/"+parts[1]) {
		return httpx.NotFound("no such asset")
	}
	rel := path.Clean(parts[2])
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return httpx.NotFound("no such asset")
	}
	file := protocol.LessonDir(parts[0]+"/"+parts[1]) + "/" + protocol.BundleAssetsDir + "/" + rel
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(rel, ".svg") {
		// SVG can carry script; render it only as an image.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	return s.serveFile(w, r, root, file, "", "public, max-age=3600")
}

func (s *Server) scenario(w http.ResponseWriter, r *http.Request) error {
	root, err := s.rootOrErr()
	if err != nil {
		return err
	}
	id, err := s.lessonID(r)
	if err != nil {
		return err
	}
	return s.serveFile(w, r, root, protocol.LessonDir(id)+"/"+protocol.BundleScenario, "application/json; charset=utf-8", "no-cache")
}

func (s *Server) fixtures(w http.ResponseWriter, r *http.Request) error {
	root, err := s.rootOrErr()
	if err != nil {
		return err
	}
	id, err := s.lessonID(r)
	if err != nil {
		return err
	}
	if _, err := root.Stat(protocol.LessonDir(id) + "/" + protocol.BundleScenario); err != nil {
		return httpx.NotFound("no such lesson")
	}
	file := protocol.FixturePath(id)
	if _, err := root.Stat(file); errors.Is(err, fs.ErrNotExist) {
		w.Header().Set("Cache-Control", "no-cache")
		httpx.NoContent(w)
		return nil
	}
	return s.serveFile(w, r, root, file, protocol.FixturesContentType, "no-cache")
}

// serveFile serves name from root with a content ETag.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, root *os.Root, name, ctype, cache string) error {
	f, err := root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return httpx.NotFound("not found")
		}
		// Escapes through symlinks or "..": treat as missing.
		return httpx.NotFound("not found").Wrap(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return httpx.NotFound("not found")
	}
	etag, err := s.etag(name, st, f)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", cache)
	if ctype != "" {
		h.Set("Content-Type", ctype)
	}
	http.ServeContent(w, r, path.Base(name), st.ModTime(), f)
	return nil
}

func (s *Server) etag(name string, st fs.FileInfo, f io.Reader) (string, error) {
	s.etagMu.Lock()
	e, ok := s.etags[name]
	s.etagMu.Unlock()
	if ok && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		return e.etag, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
	s.etagMu.Lock()
	s.etags[name] = etagEntry{size: st.Size(), mtime: st.ModTime(), etag: etag}
	s.etagMu.Unlock()
	return etag, nil
}

// ReadScenario returns a lesson's scenario.json (stuck reports, tests).
func (s *Server) ReadScenario(lessonID string) (*protocol.Scenario, error) {
	root, err := s.rootOrErr()
	if err != nil {
		return nil, err
	}
	if !lessonIDRe.MatchString(lessonID) {
		return nil, fs.ErrNotExist
	}
	data, err := root.ReadFile(protocol.LessonDir(lessonID) + "/" + protocol.BundleScenario)
	if err != nil {
		return nil, err
	}
	var sc protocol.Scenario
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&sc); err != nil {
		return nil, err
	}
	return &sc, nil
}
