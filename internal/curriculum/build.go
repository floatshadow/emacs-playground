package curriculum

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/zsy/emacs-playground/internal/protocol"
)

// BuildOptions configure Build.
type BuildOptions struct {
	// Version identifies the build ("2026.10.26+fe00636"); default: the
	// build date.
	Version string
	// BuiltAt is the build time; default: SOURCE_DATE_EPOCH or now.
	BuiltAt time.Time
}

// Build writes the bundle for src into out (protocol/curriculum.go layout).
// Callers must not build a source whose Load reported errors. Existing curriculum*.json,
// lessons/ and fixtures/ in out are replaced; other files are left alone.
func Build(src *Source, out string, opt BuildOptions) error {
	if opt.BuiltAt.IsZero() {
		opt.BuiltAt = time.Now().UTC()
	}
	if opt.Version == "" {
		opt.Version = opt.BuiltAt.Format("2006.01.02")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, d := range []string{protocol.BundleLessonsDir, protocol.BundleFixturesDir} {
		if err := os.RemoveAll(filepath.Join(out, d)); err != nil {
			return err
		}
	}
	old, _ := filepath.Glob(filepath.Join(out, "curriculum*.json"))
	for _, f := range old {
		_ = os.Remove(f)
	}

	for _, loc := range append([]string{protocol.DefaultLocale}, src.Locales...) {
		idx := BuildIndex(src, loc, opt)
		if err := writeJSON(filepath.Join(out, protocol.BundleIndexName(loc)), idx); err != nil {
			return err
		}
	}
	for _, id := range src.Order {
		l := src.Lessons[id]
		if l == nil {
			continue
		}
		if err := buildLesson(l, src.Locales, out); err != nil {
			return fmt.Errorf("lesson %s: %w", id, err)
		}
	}
	return nil
}

// BuildIndex returns curriculum.json for locale: index.yaml with lesson
// metadata from each scenario and the tables.
func BuildIndex(src *Source, locale string, opt BuildOptions) protocol.CurriculumIndex {
	idx := protocol.CurriculumIndex{
		Format:   protocol.CurriculumFormat,
		Version:  opt.Version,
		BuiltAt:  opt.BuiltAt.UnixMilli(),
		Locale:   locale,
		Parts:    []protocol.Part{},
		Projects: []protocol.ProjectRef{},
	}
	if t := src.Tables[locale]; t != nil {
		idx.Tables = *t
	} else if t := src.Tables[protocol.DefaultLocale]; t != nil {
		idx.Tables = *t
	}
	normaliseTables(&idx.Tables)
	for _, p := range src.Index.Parts {
		np := p
		np.Lessons = make([]protocol.LessonRef, 0, len(p.Lessons))
		if np.Modules == nil {
			np.Modules = []string{}
		}
		for _, ref := range p.Lessons {
			np.Lessons = append(np.Lessons, lessonRef(src, ref, locale))
		}
		idx.Parts = append(idx.Parts, np)
	}
	for _, pr := range src.Index.Projects {
		np := pr
		if l := src.Lessons[pr.ID]; l != nil {
			s := &l.Scenario
			if t := s.Title.Get(locale); t != "" {
				np.Title = t
			}
			if s.Minutes > 0 {
				np.Minutes = s.Minutes
			}
			if s.VSCode != "" {
				np.VSCode = s.VSCode
			}
			if len(s.Tags) > 0 {
				np.Tags = s.Tags
			}
			np.Status = protocol.Ready
			// Task titles from the step goals when the index has none.
			for i := range np.Tasks {
				if np.Tasks[i].Title == "" {
					if si := s.StepIndex(np.Tasks[i].ID); si >= 0 {
						np.Tasks[i].Title = s.Steps[si].Goal.Get(locale)
					}
				}
			}
		}
		if np.Languages == nil {
			np.Languages = []string{}
		}
		if np.Tasks == nil {
			np.Tasks = []protocol.ProjectTask{}
		}
		idx.Projects = append(idx.Projects, np)
	}
	return idx
}

func lessonRef(src *Source, ref protocol.LessonRef, locale string) protocol.LessonRef {
	out := ref
	l := src.Lessons[ref.ID]
	if l == nil {
		out.Status = protocol.Planned
		out.Steps = 0
		return out
	}
	s := &l.Scenario
	if t := s.Title.Get(locale); t != "" {
		out.Title = t
	}
	if s.Minutes > 0 {
		out.Minutes = s.Minutes
	}
	if s.Mode != "" {
		out.Mode = s.Mode
	} else {
		out.Mode = protocol.LessonTerminal
	}
	if len(s.Tags) > 0 {
		out.Tags = s.Tags
	}
	if s.VSCode != "" {
		out.VSCode = s.VSCode
	}
	out.Status = protocol.Ready
	out.Steps = len(s.Steps)
	return out
}

// LessonBundle returns the bundle of l for locale (the default locale's text
// when the lesson has no translation).
func LessonBundle(l *Lesson, locale string) protocol.LessonBundle {
	md, ok := l.Markdown[locale]
	mdLocale := locale
	if !ok {
		md, mdLocale = l.Markdown[protocol.DefaultLocale], protocol.DefaultLocale
	}
	front := l.Front[mdLocale]
	title := l.Scenario.Title.Get(locale)
	if title == "" {
		title = front.Title
	}
	if front.Title == "" {
		front.Title = title
	}
	assets := l.Assets
	if assets == nil {
		assets = []string{}
	}
	return protocol.LessonBundle{
		ID:       l.ID,
		Title:    title,
		Locale:   mdLocale,
		Markdown: md,
		Front:    front,
		Scenario: l.Scenario,
		Assets:   assets,
	}
}

func buildLesson(l *Lesson, locales []string, out string) error {
	dir := filepath.Join(out, filepath.FromSlash(protocol.LessonDir(l.ID)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, protocol.BundleLessonFile), LessonBundle(l, protocol.DefaultLocale)); err != nil {
		return err
	}
	for _, loc := range locales {
		if _, ok := l.Markdown[loc]; !ok {
			continue
		}
		if err := writeJSON(filepath.Join(dir, protocol.BundleLessonName(loc)), LessonBundle(l, loc)); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(dir, protocol.BundleScenario), l.Scenario); err != nil {
		return err
	}
	for _, a := range l.Assets {
		if err := copyFile(filepath.Join(l.Dir, AssetsDir, filepath.FromSlash(a)), filepath.Join(dir, protocol.BundleAssetsDir, filepath.FromSlash(a))); err != nil {
			return err
		}
	}
	if l.FixtureDir != "" {
		tarball := filepath.Join(out, filepath.FromSlash(protocol.FixturePath(l.ID)))
		if err := os.MkdirAll(filepath.Dir(tarball), 0o755); err != nil {
			return err
		}
		f, err := os.Create(tarball)
		if err != nil {
			return err
		}
		if err := WriteFixtures(f, l.FixtureDir); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// WriteFixtures writes dir as a reproducible zstd-compressed tar archive to
// w: entries sorted by path, directories with a trailing slash, mtimes zero,
// uid and gid 0, permission bits preserved. Only regular files and
// directories are allowed.
func WriteFixtures(w io.Writer, dir string) error {
	type entry struct {
		rel  string
		info fs.FileInfo
	}
	var entries []entry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("fixture %s: only regular files and directories are allowed", p)
		}
		rel, _ := filepath.Rel(dir, p)
		entries = append(entries, entry{filepath.ToSlash(rel), info})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	zw, err := zstd.NewWriter(w, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	epoch := time.Unix(0, 0).UTC()
	for _, e := range entries {
		h := &tar.Header{
			Name:    e.rel,
			Mode:    int64(e.info.Mode().Perm()),
			ModTime: epoch,
			Format:  tar.FormatPAX,
		}
		if e.info.IsDir() {
			h.Typeflag = tar.TypeDir
			h.Name += "/"
		} else {
			h.Typeflag = tar.TypeReg
			h.Size = e.info.Size()
		}
		if len(h.Name) <= 100 {
			h.Format = tar.FormatUSTAR
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !e.info.IsDir() {
			f, err := os.Open(filepath.Join(dir, filepath.FromSlash(e.rel)))
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

// FixtureFile is one regular file read back from a fixture tarball.
type FixtureFile struct {
	Name string
	Mode fs.FileMode
	Data []byte
}

// ReadFixtures decodes a fixture tarball into its regular files (directories
// are implied). It rejects absolute paths, "..", links and special files,
// and files larger than maxFile bytes (0: 64 MB).
func ReadFixtures(r io.Reader, maxFile int64) ([]FixtureFile, error) {
	if maxFile <= 0 {
		maxFile = 64 << 20
	}
	zr, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var out []FixtureFile
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || strings.HasSuffix(name, "/..") {
			return nil, fmt.Errorf("fixtures: unsafe path %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("fixtures: %q: only regular files and directories are allowed", h.Name)
		}
		if h.Size > maxFile {
			return nil, fmt.Errorf("fixtures: %q is larger than %d bytes", h.Name, maxFile)
		}
		var b bytes.Buffer
		if _, err := io.Copy(&b, io.LimitReader(tr, maxFile+1)); err != nil {
			return nil, err
		}
		out = append(out, FixtureFile{Name: name, Mode: fs.FileMode(h.Mode).Perm(), Data: b.Bytes()})
	}
}

func writeJSON(path string, v any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
