package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"

	"github.com/floatshadow/emacs-playground/internal/config"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// configSchema prints the JSON Schema of the gateway configuration file
// (deploy/config.schema.json, deployment.md §3).
func configSchema(args []string) int {
	fs := flag.NewFlagSet("config schema", flag.ExitOnError)
	out := fs.String("o", "", "output file (default stdout)")
	_ = fs.Parse(args)
	data, err := ConfigSchema()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *out == "" {
		_, _ = os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// ConfigSchema reflects protocol.Config into a JSON Schema. Every key is
// optional (all have defaults) and unknown keys are rejected, matching
// config.Decode. Descriptions come from the Go doc comments when the
// source is available (running from the repository root).
func ConfigSchema() ([]byte, error) {
	r := &jsonschema.Reflector{
		RequiredFromJSONSchemaTags: true,
		DoNotReference:             true,
		ExpandedStruct:             true,
	}
	// Best effort: comments need the source tree.
	_ = r.AddGoComments("github.com/floatshadow/emacs-playground", "./internal/protocol")
	s := r.Reflect(&protocol.Config{})
	s.ID = "https://github.com/floatshadow/emacs-playground/deploy/config.schema.json"
	s.Title = "Emacs Playground gateway configuration"
	if s.Description == "" {
		s.Description = "deploy/config.json (deployment.md §3). Every key is optional; defaults apply."
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// configCheck validates a configuration file without starting anything.
func configCheck(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: playctl config check <file>")
		return 2
	}
	cfg, err := config.LoadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s: ok (mode %s, auth %s, sessions %s)\n", args[0], cfg.Mode, cfg.Auth.Mode, cfg.Sessions.Backend)
	return 0
}
