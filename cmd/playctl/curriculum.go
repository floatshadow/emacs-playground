package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zsy/emacs-playground/internal/curriculum"
)

// curriculumValidate implements `playctl curriculum validate [dir]`
// (CURRICULUM.md Part B.5): prints every diagnostic and exits 1 on errors
// (or on warnings with -strict).
func curriculumValidate(args []string) int {
	fs := flag.NewFlagSet("curriculum validate", flag.ExitOnError)
	strict := fs.Bool("strict", false, "treat warnings as errors")
	quiet := fs.Bool("q", false, "print errors only")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	dir := "curriculum"
	if len(pos) > 0 {
		dir = pos[0]
	}
	src, diags := curriculum.Load(dir)
	for _, d := range diags {
		if *quiet && d.Severity == curriculum.Warning {
			continue
		}
		fmt.Fprintln(os.Stderr, d)
	}
	nerr, nwarn := diags.Count(curriculum.Error), diags.Count(curriculum.Warning)
	steps := 0
	for _, l := range src.Lessons {
		steps += len(l.Scenario.Steps)
	}
	fmt.Printf("%s: %d lessons (%d steps), %d locales, %d errors, %d warnings\n",
		dir, len(src.Lessons), steps, len(src.Locales)+1, nerr, nwarn)
	if nerr > 0 || (*strict && nwarn > 0) {
		return 1
	}
	return 0
}

// curriculumBuild implements `playctl curriculum build [dir] -o out/`.
func curriculumBuild(args []string) int {
	fs := flag.NewFlagSet("curriculum build", flag.ExitOnError)
	out := fs.String("o", "", "output directory (required)")
	version := fs.String("version", os.Getenv("PG_CURRICULUM_VERSION"), "bundle version (default <date>+<git sha>)")
	lenient := fs.Bool("lenient", false, "build anyway, dropping lessons with errors (they show as planned)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: playctl curriculum build [dir] -o out/")
		return 2
	}
	dir := "curriculum"
	if len(pos) > 0 {
		dir = pos[0]
	}
	src, diags := curriculum.Load(dir)
	for _, d := range diags {
		if d.Severity == curriculum.Error {
			fmt.Fprintln(os.Stderr, d)
		}
	}
	if diags.HasErrors() {
		if !*lenient {
			fmt.Fprintf(os.Stderr, "%s: %d errors; fix them or build with -lenient\n", dir, diags.Count(curriculum.Error))
			return 1
		}
		dropped := dropBrokenLessons(src, diags)
		fmt.Fprintf(os.Stderr, "-lenient: building without %d lesson(s) that have errors: %s\n", len(dropped), strings.Join(dropped, ", "))
	}
	builtAt := time.Now().UTC()
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			builtAt = time.Unix(n, 0).UTC()
		}
	}
	if *version == "" {
		*version = builtAt.Format("2006.01.02")
		if sha := gitShortSHA(dir); sha != "" {
			*version += "+" + sha
		}
	}
	if err := curriculum.Build(src, *out, curriculum.BuildOptions{Version: *version, BuiltAt: builtAt}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fixtures := 0
	for _, l := range src.Lessons {
		if l.FixtureDir != "" {
			fixtures++
		}
	}
	fmt.Printf("%s → %s: version %s, %d lessons, %d fixture tarballs, locales en %s\n",
		dir, *out, *version, len(src.Lessons), fixtures, strings.Join(src.Locales, " "))
	return 0
}

// dropBrokenLessons removes lessons with error diagnostics in their files.
func dropBrokenLessons(src *curriculum.Source, diags curriculum.Diagnostics) []string {
	var dropped []string
	for id := range src.Lessons {
		for _, d := range diags {
			if d.Severity == curriculum.Error && strings.HasPrefix(d.File, id+"/") {
				delete(src.Lessons, id)
				dropped = append(dropped, id)
				break
			}
		}
	}
	return dropped
}

func gitShortSHA(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = abs
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
