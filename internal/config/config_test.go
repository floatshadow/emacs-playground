package config

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestDefaultsValid(t *testing.T) {
	cfg, err := Load(Sources{File: "", Env: nil})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sessions.Backend != protocol.BackendStatic || cfg.Auth.CookieSecretFile != "/data/cookie.secret" {
		t.Fatalf("resolve: %+v", cfg.Sessions)
	}
}

func TestExampleConfigs(t *testing.T) {
	for _, p := range []string{"../../deploy/config.example.json", "../../deploy/config.json"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if _, err := LoadFile(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	_ = os.WriteFile(p, []byte(`{"mode":"local","listen":":1","sessions":{"limits":{"memory":"2g"}}}`), 0o644)
	env := []string{
		"PG_SESSIONS_LIMITS_MEMORY=4g",
		"PG_PUBLIC_URL=https://emacs.example.edu",
		"PG_MAX_SESSIONS=7",
		"PG_AUTH_OIDC_ALLOWED_EMAIL_DOMAINS=a.edu, b.edu",
		"PG_SESSION_ID=ignored-by-gateway",
		"HOME=/x",
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	f := BindFlags(fs)
	if err := fs.Parse([]string{"-config", p, "-set", "sessions.limits.cpus=3.5", "-listen", ":9"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(f.Sources(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9" || cfg.Sessions.Limits.Memory != "4g" || cfg.Sessions.Limits.CPUs != 3.5 ||
		cfg.PublicURL != "https://emacs.example.edu" || cfg.Sessions.MaxSessions != 7 ||
		strings.Join(cfg.Auth.OIDC.AllowedEmailDomains, "|") != "a.edu|b.edu" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestStrictFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	_ = os.WriteFile(p, []byte(`{"mode":"local","sesions":{}}`), 0o644)
	if _, err := LoadFile(p); err == nil {
		t.Fatal("want unknown key error")
	}
}

func TestValidate(t *testing.T) {
	cfg := Defaults()
	cfg.Mode = protocol.ModeServer
	Resolve(&cfg)
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "auth.mode: none") {
		t.Fatalf("want auth none error, got %v", err)
	}
	cfg.Auth.Mode = protocol.AuthInvite
	cfg.Auth.Invites.File = "/etc/playground/invites.txt"
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Sessions.Limits.Memory = "lots"
	cfg.Sessions.GUI.Geometry = "big"
	err = Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "memory") || !strings.Contains(err.Error(), "geometry") {
		t.Fatalf("got %v", err)
	}
}

func TestEnvNames(t *testing.T) {
	cases := map[string]string{
		"sessions.limits.memory":        "PG_SESSIONS_LIMITS_MEMORY",
		"publicUrl":                     "PG_PUBLIC_URL",
		"sessions.idleStopMinutes":      "PG_SESSIONS_IDLE_STOP_MINUTES",
		"auth.oidc.clientId":            "PG_AUTH_OIDC_CLIENT_ID",
		"local.sessionUrl":              "PG_LOCAL_SESSION_URL",
		"sessions.gui.geometry":         "PG_SESSIONS_GUI_GEOMETRY",
		"auth.oidc.allowedEmailDomains": "PG_AUTH_OIDC_ALLOWED_EMAIL_DOMAINS",
		"sessions.limits.cpus":          "PG_SESSIONS_LIMITS_CPUS",
	}
	for path, want := range cases {
		if got := EnvName(path); got != want {
			t.Errorf("EnvName(%s) = %s, want %s", path, got, want)
		}
	}
	seen := map[string]string{}
	for _, k := range Keys() {
		if prev, ok := seen[k.Env]; ok {
			t.Fatalf("env collision %s: %s and %s", k.Env, prev, k.Path)
		}
		seen[k.Env] = k.Path
	}
}

func TestRedacted(t *testing.T) {
	cfg := Defaults()
	r := Redacted(&cfg)
	local := r["local"].(map[string]any)
	if local["sessionToken"] != "<redacted>" || local["sessionId"] != "local" {
		b, _ := json.Marshal(r)
		t.Fatalf("not redacted: %s", b)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{"3g": 3 << 30, "512m": 512 << 20, "1024": 1024, "1.5g": 3 << 29, "4GB": 4 << 30}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%s) = %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "g", "-1g", "x"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q): want error", bad)
		}
	}
}

func TestSessiond(t *testing.T) {
	cfg, err := LoadSessiond([]string{"PG_SESSION_ID=abc", "PG_VNC_PORT=5902", "PG_GUI_ENABLED=false"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionID != "abc" || cfg.VNCPort != 5902 || cfg.GUIEnabled || cfg.Listen != ":7000" || cfg.Workspace != "/work" {
		t.Fatalf("%+v", cfg)
	}
	back, err := LoadSessiond(sessiondEnv(cfg))
	if err != nil || *back != *cfg {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if _, err := LoadSessiond([]string{"PG_TERM_SIZE=wide"}); err == nil {
		t.Fatal("want error")
	}
}

// sessiondEnv renders cfg back to environment variables (the inverse of
// LoadSessiond).
func sessiondEnv(cfg *protocol.SessiondConfig) []string {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	var out []string
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("env")
		if name == "" {
			continue
		}
		fv := v.Field(i)
		var s string
		switch fv.Kind() {
		case reflect.Bool:
			s = strconv.FormatBool(fv.Bool())
		case reflect.Int, reflect.Int64:
			s = strconv.FormatInt(fv.Int(), 10)
		default:
			s = fv.String()
		}
		out = append(out, name+"="+s)
	}
	return out
}
