package curriculum

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// SplitFrontMatter separates the YAML front matter of a lesson text from its
// body. The front matter is the block between a first line "---" and the
// next line "---". A text without front matter yields an empty front and an
// error (title is required).
func SplitFrontMatter(data []byte) (protocol.LessonFront, string, error) {
	var front protocol.LessonFront
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return front, text, fmt.Errorf("missing front matter (--- title: … ---)")
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---\n")
	var yamlPart, body string
	switch {
	case end >= 0:
		yamlPart, body = rest[:end], rest[end+5:]
	case strings.HasSuffix(rest, "\n---"):
		yamlPart, body = strings.TrimSuffix(rest, "\n---"), ""
	case strings.HasPrefix(rest, "---\n"):
		yamlPart, body = "", rest[4:]
	default:
		return front, text, fmt.Errorf("front matter is not closed with ---")
	}
	err := DecodeYAML([]byte(yamlPart), &front)
	return front, strings.TrimLeft(body, "\n"), err
}

func validateFront(f protocol.LessonFront, r reporter) {
	if strings.TrimSpace(f.Title) == "" {
		r.errorf("front matter", "title is required")
	}
	if f.Minutes < 0 {
		r.errorf("front matter", "minutes must not be negative")
	}
}

var (
	// :kbd[C-x C-f] and :try[C-x C-f] (CURRICULUM.md Part B.2).
	keyDirectiveRe = regexp.MustCompile(`:(kbd|try)\[([^\]]*)\]`)
	jargonRe       = regexp.MustCompile(`:jargon\[([^\]]*)\]`)
	// Container directives (:::name) and leaf directives (::name).
	containerRe = regexp.MustCompile(`(?m)^[ \t]*:::[ \t]*([A-Za-z][\w-]*)`)
	leafRe      = regexp.MustCompile(`(?m)^[ \t]*::([A-Za-z][\w-]*)`)
	imageRe     = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)`)
	fenceRe     = regexp.MustCompile("(?m)^\\s*(```|~~~)")
)

var knownContainers = map[string]bool{"vscode": true, "aside": true, "gui-only": true}
var knownLeaves = map[string]bool{"steps": true}

// validateMarkdown checks the directives of a lesson text.
func validateMarkdown(body string, r reporter) {
	text := stripFences(body)
	for _, m := range keyDirectiveRe.FindAllStringSubmatch(text, -1) {
		if _, err := protocol.ParseKeys(m[2]); err != nil {
			r.errorf(":"+m[1]+"["+m[2]+"]", "not a key sequence: %v", err)
		}
	}
	for _, m := range containerRe.FindAllStringSubmatch(text, -1) {
		if !knownContainers[m[1]] {
			r.warnf(":::"+m[1], "unknown container directive (known: vscode, aside, gui-only)")
		}
	}
	for _, m := range leafRe.FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[0], ":::") {
			continue
		}
		if !knownLeaves[m[1]] {
			r.warnf("::"+m[1], "unknown leaf directive (known: steps)")
		}
	}
	// Container directives must be balanced: every ":::name" opens, a bare
	// ":::" closes.
	open := 0
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, ":::") {
			continue
		}
		if strings.TrimSpace(strings.TrimLeft(t, ":")) == "" {
			open--
		} else {
			open++
		}
	}
	if open != 0 {
		r.errorf("directives", "unbalanced ::: container directives (%d unclosed)", open)
	}
}

// checkJargon warns about :jargon[term] without a glossary entry.
func checkJargon(body string, glossary map[string]string, r reporter) {
	lower := map[string]bool{}
	for k := range glossary {
		lower[strings.ToLower(k)] = true
	}
	for _, m := range jargonRe.FindAllStringSubmatch(stripFences(body), -1) {
		// Same lookup as web/src/components/lesson/Markdown.tsx: exact
		// (case-insensitive), then with a trailing plural "s" removed.
		term := strings.ToLower(strings.TrimSpace(m[1]))
		if !lower[term] && !lower[strings.TrimSuffix(term, "s")] {
			r.warnf(":jargon["+m[1]+"]", "term is not in tables/glossary.yaml")
		}
	}
}

// checkImages checks that relative image paths point into assets/.
func checkImages(l *Lesson, r reporter) {
	for _, body := range l.Markdown {
		for _, m := range imageRe.FindAllStringSubmatch(stripFences(body), -1) {
			ref := m[1]
			if strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
				continue
			}
			clean := path.Clean(strings.TrimPrefix(ref, "./"))
			if !strings.HasPrefix(clean, AssetsDir+"/") {
				r.errorf("image "+ref, "images must be relative paths into assets/")
				continue
			}
			if _, err := os.Stat(filepath.Join(l.Dir, filepath.FromSlash(clean))); err != nil {
				r.errorf("image "+ref, "file not found")
			}
		}
	}
}

// stripFences blanks fenced code blocks so directives inside them are not
// checked.
func stripFences(body string) string {
	if !fenceRe.MatchString(body) {
		return body
	}
	var b bytes.Buffer
	in := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			in = !in
			b.WriteByte('\n')
			continue
		}
		if !in {
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
