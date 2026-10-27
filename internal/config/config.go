// Package config loads the gateway configuration (deployment.md §3) and
// sessiond's environment.
//
// Gateway precedence, lowest to highest: Defaults, the JSON file, PG_*
// environment variables, then flags (-set key.path=value and the shortcut
// flags). Environment variable names are derived from the JSON path by
// splitting camelCase words and joining with underscores:
// sessions.limits.memory → PG_SESSIONS_LIMITS_MEMORY, publicUrl →
// PG_PUBLIC_URL, sessions.idleStopMinutes → PG_SESSIONS_IDLE_STOP_MINUTES.
// The unsplit form (PG_PUBLICURL) and the short aliases in Aliases are also
// accepted. List values are comma-separated.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// DefaultPath is where the gateway looks for the configuration file when
// neither -config nor PG_CONFIG is given.
const DefaultPath = "/etc/playground/config.json"

// EnvPrefix prefixes every configuration environment variable.
const EnvPrefix = "PG_"

// Defaults returns the built-in defaults (deployment.md §3). Fields whose
// default depends on other fields (sessions.backend, auth.cookieSecretFile)
// are resolved by Resolve after all sources are applied.
func Defaults() protocol.Config {
	return protocol.Config{
		Mode:      protocol.ModeLocal,
		Listen:    ":8080",
		PublicURL: "http://localhost:8080",
		DataDir:   "/data",
		Auth: protocol.AuthConfig{
			Mode:        protocol.AuthNone,
			SessionDays: 30,
		},
		Sessions: protocol.SessionsConfig{
			Image:               "ghcr.io/floatshadow/emacs-playground/session:latest",
			Network:             "emacs-playground_sessions",
			Isolation:           protocol.IsolationNetwork,
			SubnetPool:          "10.231.0.0/16",
			Runtime:             "runc",
			Limits:              protocol.LimitsConfig{CPUs: 2, Memory: "3g", Pids: 1024, Disk: "4g"},
			IdleStopMinutes:     30,
			MaxSessions:         50,
			VolumeRetentionDays: 30,
			Egress:              protocol.EgressRegistries,
			RemoteBox:           protocol.RemoteBoxConfig{Enabled: true, Image: "ghcr.io/floatshadow/emacs-playground/remote-box:latest"},
			GUI:                 protocol.GUIConfig{Enabled: true, Geometry: "1600x900"},
			Terminal:            protocol.TerminalConfig{InitialSize: "120x36"},
		},
		Local: protocol.LocalConfig{
			SessionURL:   "http://session:7000",
			Workspace:    "./workspace",
			SessionID:    "local",
			SessionToken: "local",
		},
		Curriculum: protocol.CurriculumConfig{Dir: "/srv/curriculum", DefaultLocale: "en"},
		Logging:    protocol.LoggingConfig{Level: "info", Format: "json"},
		Metrics:    protocol.MetricsConfig{Enabled: false, Listen: ":9090"},
		Features:   protocol.FeaturesConfig{StuckReports: true},
	}
}

// Aliases maps documented short environment names to configuration paths
// (names used in architecture.md and backend.md).
var Aliases = map[string]string{
	"PG_IDLE_STOP_MINUTES":     "sessions.idleStopMinutes",
	"PG_VOLUME_RETENTION_DAYS": "sessions.volumeRetentionDays",
	"PG_MAX_SESSIONS":          "sessions.maxSessions",
	"PG_SESSION_URL":           "local.sessionUrl",
	"PG_REMOTE_IMAGE":          "sessions.remoteBox.image",
	"PG_EGRESS_MODE":           "sessions.egress",
	"PG_COOKIE_SECRET_FILE":    "auth.cookieSecretFile",
	"PG_AUTH_MODE":             "auth.mode",
	"PG_LOG_LEVEL":             "logging.level",
	"PG_LOG_FORMAT":            "logging.format",
	"PG_SESSION_IMAGE":         "sessions.image",
	"PG_DOCKER_HOST":           "sessions.dockerHost",
}

// Sources says where Load reads from.
type Sources struct {
	// File is the configuration file path. Empty: PG_CONFIG from Env, else
	// DefaultPath if it exists, else no file.
	File string
	// RequireFile makes a missing file an error even when File is empty.
	RequireFile bool
	// Env is the environment as KEY=VALUE pairs (os.Environ()).
	Env []string
	// Set are "key.path=value" overrides applied last (flags).
	Set []string
}

// Load builds the effective configuration from defaults, file, environment
// and flags, resolves derived defaults and validates the result.
func Load(src Sources) (*protocol.Config, error) {
	cfg := Defaults()
	env := envMap(src.Env)
	path := src.File
	explicit := path != ""
	if path == "" {
		if p := env["PG_CONFIG"]; p != "" {
			path, explicit = p, true
		} else {
			path = DefaultPath
		}
	}
	if err := applyFile(&cfg, path, explicit || src.RequireFile); err != nil {
		return nil, err
	}
	if err := ApplyEnv(&cfg, src.Env); err != nil {
		return nil, err
	}
	for _, kv := range src.Set {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("config: -set %q: want key.path=value", kv)
		}
		if err := Set(&cfg, strings.TrimSpace(k), v); err != nil {
			return nil, err
		}
	}
	Resolve(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadFile decodes a configuration file over the defaults without
// environment or flags (playctl config check).
func LoadFile(path string) (*protocol.Config, error) {
	cfg := Defaults()
	if err := applyFile(&cfg, path, true); err != nil {
		return nil, err
	}
	Resolve(&cfg)
	return &cfg, Validate(&cfg)
}

func applyFile(cfg *protocol.Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}
		return fmt.Errorf("config: %w", err)
	}
	return Decode(cfg, data)
}

// Decode strictly decodes JSON configuration data over cfg: unknown keys are
// errors, so typos do not silently fall back to defaults.
func Decode(cfg *protocol.Config, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// Resolve fills defaults that depend on other fields.
func Resolve(cfg *protocol.Config) {
	if cfg.Sessions.Backend == "" {
		if cfg.Mode == protocol.ModeServer {
			cfg.Sessions.Backend = protocol.BackendDocker
		} else {
			cfg.Sessions.Backend = protocol.BackendStatic
		}
	}
	if cfg.Auth.CookieSecretFile == "" {
		cfg.Auth.CookieSecretFile = strings.TrimRight(cfg.DataDir, "/") + "/cookie.secret"
	}
}

var (
	geometryRe = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)
	levels     = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	formats    = map[string]bool{"json": true, "text": true}
)

// Validate checks the configuration and returns all problems joined.
func Validate(cfg *protocol.Config) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch cfg.Mode {
	case protocol.ModeLocal, protocol.ModeServer:
	default:
		bad("mode: %q is not local or server", cfg.Mode)
	}
	if cfg.Listen == "" {
		bad("listen: required")
	}
	if u, err := url.Parse(cfg.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		bad("publicUrl: %q is not an absolute http(s) URL", cfg.PublicURL)
	}
	if cfg.DataDir == "" {
		bad("dataDir: required")
	}
	switch cfg.Auth.Mode {
	case protocol.AuthNone:
		if cfg.Mode != protocol.ModeLocal {
			bad("auth.mode: none is only allowed in local mode")
		}
	case protocol.AuthInvite:
		if cfg.Auth.Invites.File == "" {
			bad("auth.invites.file: required when auth.mode is invite")
		}
	case protocol.AuthOIDC:
		if cfg.Auth.OIDC.Issuer == "" || cfg.Auth.OIDC.ClientID == "" || cfg.Auth.OIDC.ClientSecretFile == "" {
			bad("auth.oidc: issuer, clientId and clientSecretFile are required when auth.mode is oidc")
		}
	default:
		bad("auth.mode: %q is not none, invite or oidc", cfg.Auth.Mode)
	}
	if cfg.Auth.SessionDays < 1 {
		bad("auth.sessionDays: must be at least 1")
	}
	s := cfg.Sessions
	switch s.Backend {
	case protocol.BackendStatic:
		if u, err := url.Parse(cfg.Local.SessionURL); err != nil || u.Scheme == "" || u.Host == "" {
			bad("local.sessionUrl: %q is not an absolute URL", cfg.Local.SessionURL)
		}
		if cfg.Local.SessionID == "" || cfg.Local.SessionToken == "" {
			bad("local.sessionId and local.sessionToken: required with the static backend")
		}
	case protocol.BackendDocker:
		if s.Image == "" {
			bad("sessions.image: required with the docker backend")
		}
		switch s.Isolation {
		case protocol.IsolationNetwork:
			if s.SubnetPool != "" {
				if p, err := netip.ParsePrefix(s.SubnetPool); err != nil || !p.Addr().Is4() || p.Bits() > 28 {
					bad("sessions.subnetPool: %q is not an IPv4 range of /28 or larger", s.SubnetPool)
				}
			}
		case protocol.IsolationShared:
			if s.Network == "" {
				bad("sessions.network: required with isolation shared")
			}
		default:
			bad("sessions.isolation: %q is not network or shared", s.Isolation)
		}
		if s.RemoteBox.Enabled && s.RemoteBox.Image == "" {
			bad("sessions.remoteBox.image: required when the remote box is enabled")
		}
	default:
		bad("sessions.backend: %q is not static or docker", s.Backend)
	}
	if s.Runtime == "" {
		bad("sessions.runtime: required")
	}
	if s.Limits.CPUs <= 0 {
		bad("sessions.limits.cpus: must be positive")
	}
	if _, err := ParseSize(s.Limits.Memory); err != nil {
		bad("sessions.limits.memory: %v", err)
	}
	if s.Limits.Disk != "" {
		if _, err := ParseSize(s.Limits.Disk); err != nil {
			bad("sessions.limits.disk: %v", err)
		}
	}
	if s.Limits.Pids <= 0 {
		bad("sessions.limits.pids: must be positive")
	}
	if s.IdleStopMinutes < 0 || s.VolumeRetentionDays < 0 {
		bad("sessions.idleStopMinutes and volumeRetentionDays: must not be negative")
	}
	if s.MaxSessions < 1 {
		bad("sessions.maxSessions: must be at least 1")
	}
	switch s.Egress {
	case protocol.EgressNone, protocol.EgressRegistries, protocol.EgressAll:
	default:
		bad("sessions.egress: %q is not none, registries or all", s.Egress)
	}
	if !geometryRe.MatchString(s.GUI.Geometry) {
		bad("sessions.gui.geometry: %q is not WIDTHxHEIGHT", s.GUI.Geometry)
	}
	if !geometryRe.MatchString(s.Terminal.InitialSize) {
		bad("sessions.terminal.initialSize: %q is not COLSxROWS", s.Terminal.InitialSize)
	}
	if cfg.Curriculum.Dir == "" || cfg.Curriculum.DefaultLocale == "" {
		bad("curriculum.dir and curriculum.defaultLocale: required")
	}
	if !levels[cfg.Logging.Level] {
		bad("logging.level: %q is not debug, info, warn or error", cfg.Logging.Level)
	}
	if !formats[cfg.Logging.Format] {
		bad("logging.format: %q is not json or text", cfg.Logging.Format)
	}
	if cfg.Metrics.Enabled && cfg.Metrics.Listen == "" {
		bad("metrics.listen: required when metrics are enabled")
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("config: %w", errors.Join(errs...))
}

// ParseSize parses a Docker-style size ("3g", "512m", "1024k", "100") into
// bytes.
func ParseSize(s string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimSuffix(t, "b")
	if t == "" {
		return 0, fmt.Errorf("size %q is empty", s)
	}
	mult := int64(1)
	switch t[len(t)-1] {
	case 'k':
		mult = 1 << 10
	case 'm':
		mult = 1 << 20
	case 'g':
		mult = 1 << 30
	case 't':
		mult = 1 << 40
	}
	if mult != 1 {
		t = t[:len(t)-1]
	}
	n, err := strconv.ParseFloat(t, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("size %q is not a positive number with an optional k, m, g or t suffix", s)
	}
	return int64(n * float64(mult)), nil
}

// ParseGeometry parses "WIDTHxHEIGHT" (or "COLSxROWS").
func ParseGeometry(s string) (w, h int, err error) {
	if !geometryRe.MatchString(s) {
		return 0, 0, fmt.Errorf("geometry %q is not WIDTHxHEIGHT", s)
	}
	a, b, _ := strings.Cut(s, "x")
	w, _ = strconv.Atoi(a)
	h, _ = strconv.Atoi(b)
	return w, h, nil
}

// Key describes one leaf configuration key.
type Key struct {
	// Path is the dotted JSON path, e.g. "sessions.limits.memory".
	Path string
	// Env is the canonical environment variable name.
	Env string
	// Kind is the Go kind of the field.
	Kind reflect.Kind
	// Secret is true for fields tagged secret:"true".
	Secret bool
}

// Keys lists every leaf key of the configuration in declaration order.
func Keys() []Key {
	var out []Key
	walk(reflect.TypeOf(protocol.Config{}), nil, func(path []string, f reflect.StructField) {
		out = append(out, Key{
			Path:   strings.Join(path, "."),
			Env:    EnvName(strings.Join(path, ".")),
			Kind:   f.Type.Kind(),
			Secret: f.Tag.Get("secret") == "true",
		})
	})
	return out
}

func walk(t reflect.Type, prefix []string, leaf func([]string, reflect.StructField)) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" || name == "$schema" {
			continue
		}
		path := append(append([]string(nil), prefix...), name)
		if f.Type.Kind() == reflect.Struct {
			walk(f.Type, path, leaf)
			continue
		}
		leaf(path, f)
	}
}

func jsonName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return name
}

// EnvName returns the canonical environment variable for a dotted path.
func EnvName(path string) string {
	segs := strings.Split(path, ".")
	for i, s := range segs {
		segs[i] = upperSnake(s)
	}
	return EnvPrefix + strings.Join(segs, "_")
}

func upperSnake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) {
			prevLower := unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if prevLower || (unicode.IsUpper(rs[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// envTable maps every accepted environment name to a path.
func envTable() map[string]string {
	t := map[string]string{}
	for _, k := range Keys() {
		t[k.Env] = k.Path
		flat := EnvPrefix + strings.ToUpper(strings.ReplaceAll(k.Path, ".", "_"))
		if _, ok := t[flat]; !ok {
			t[flat] = k.Path
		}
	}
	for name, path := range Aliases {
		t[name] = path
	}
	return t
}

// ApplyEnv applies PG_* overrides from env (KEY=VALUE pairs). Variables
// that do not name a configuration key are ignored (sessiond's variables,
// PG_CONFIG).
func ApplyEnv(cfg *protocol.Config, env []string) error {
	table := envTable()
	m := envMap(env)
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		path, ok := table[name]
		if !ok {
			continue
		}
		if err := Set(cfg, path, m[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, EnvPrefix) {
			m[k] = v
		}
	}
	return m
}

// Set assigns a string value to the field at a dotted path (case-insensitive
// segment match). Lists are comma-separated.
func Set(cfg *protocol.Config, path, value string) error {
	v := reflect.ValueOf(cfg).Elem()
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		f, ok := fieldByJSON(v, seg)
		if !ok {
			return fmt.Errorf("config: unknown key %q", path)
		}
		if i < len(segs)-1 {
			if f.Kind() != reflect.Struct {
				return fmt.Errorf("config: %q is not an object", strings.Join(segs[:i+1], "."))
			}
			v = f
			continue
		}
		return setValue(f, path, value)
	}
	return nil
}

func fieldByJSON(v reflect.Value, seg string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if strings.EqualFold(jsonName(t.Field(i)), seg) {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func setValue(f reflect.Value, path, value string) error {
	switch f.Kind() {
	case reflect.String:
		f.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("config: %s: %q is not a boolean", path, value)
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int64, reflect.Int32:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return fmt.Errorf("config: %s: %q is not an integer", path, value)
		}
		f.SetInt(n)
	case reflect.Float64, reflect.Float32:
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return fmt.Errorf("config: %s: %q is not a number", path, value)
		}
		f.SetFloat(n)
	case reflect.Slice:
		if f.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("config: %s: unsupported list type", path)
		}
		var items []string
		for _, p := range strings.Split(value, ",") {
			if p = strings.TrimSpace(p); p != "" {
				items = append(items, p)
			}
		}
		f.Set(reflect.ValueOf(items).Convert(f.Type()))
	default:
		return fmt.Errorf("config: %s: unsupported type %s", path, f.Type())
	}
	return nil
}

// Redacted returns the configuration as a JSON-ready map with secret
// fields replaced by "<redacted>" (non-empty) or "" (empty). The gateway
// logs it at start.
func Redacted(cfg *protocol.Config) map[string]any {
	return redact(reflect.ValueOf(*cfg))
}

func redact(v reflect.Value) map[string]any {
	out := map[string]any{}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" || name == "$schema" {
			continue
		}
		fv := v.Field(i)
		switch {
		case f.Type.Kind() == reflect.Struct:
			out[name] = redact(fv)
		case f.Tag.Get("secret") == "true":
			if fv.IsZero() {
				out[name] = ""
			} else {
				out[name] = "<redacted>"
			}
		default:
			out[name] = fv.Interface()
		}
	}
	return out
}

// Flags are the gateway's command-line configuration flags.
type Flags struct {
	File     string
	Set      multiFlag
	Listen   string
	Mode     string
	LogLevel string
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// BindFlags registers -config, -set (repeatable), -listen, -mode and
// -log-level on fs.
func BindFlags(fs *flag.FlagSet) *Flags {
	f := &Flags{}
	fs.StringVar(&f.File, "config", "", "configuration file (default $PG_CONFIG or "+DefaultPath+")")
	fs.Var(&f.Set, "set", "override a key: -set sessions.limits.memory=4g (repeatable)")
	fs.StringVar(&f.Listen, "listen", "", "listen address (overrides listen)")
	fs.StringVar(&f.Mode, "mode", "", "local or server (overrides mode)")
	fs.StringVar(&f.LogLevel, "log-level", "", "debug, info, warn or error (overrides logging.level)")
	return f
}

// Sources converts parsed flags and the environment into Load sources.
func (f *Flags) Sources(env []string) Sources {
	set := append([]string(nil), f.Set...)
	if f.Mode != "" {
		set = append(set, "mode="+f.Mode)
	}
	if f.Listen != "" {
		set = append(set, "listen="+f.Listen)
	}
	if f.LogLevel != "" {
		set = append(set, "logging.level="+f.LogLevel)
	}
	return Sources{File: f.File, Env: env, Set: set}
}
