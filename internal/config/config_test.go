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

func TestAuthModeDefaults(t *testing.T) {
	local := Defaults()
	Resolve(&local)
	if local.Auth.Mode != protocol.AuthNone || Validate(&local) != nil {
		t.Fatalf("local: %s %v", local.Auth.Mode, Validate(&local))
	}
	o := local.Auth.OIDC
	if strings.Join(o.Scopes, " ") != "openid email profile" || o.UsernameClaim != "preferred_username" ||
		o.GroupsClaim != "groups" || o.DisplayName == "" || !o.ProviderLogout {
		t.Fatalf("oidc defaults %+v", o)
	}

	// Server mode defaults to oidc and says what is missing.
	srv := Defaults()
	srv.Mode = protocol.ModeServer
	Resolve(&srv)
	if srv.Auth.Mode != protocol.AuthOIDC {
		t.Fatalf("server auth mode %s", srv.Auth.Mode)
	}
	err := Validate(&srv)
	if err == nil || !strings.Contains(err.Error(), "auth.oidc.issuer and auth.oidc.clientId: required") ||
		!strings.Contains(err.Error(), "server mode") || !strings.Contains(err.Error(), "/api/v1/auth/oidc/callback") {
		t.Fatalf("want missing issuer error, got %v", err)
	}
	srv.Auth.OIDC.Issuer = "https://login.example.edu/realms/main"
	srv.Auth.OIDC.ClientID = "emacs-playground"
	if err := Validate(&srv); err != nil {
		t.Fatalf("public client (no secret file) refused: %v", err)
	}

	// none is refused in server mode, even when set explicitly.
	srv.Auth.Mode = protocol.AuthNone
	if err := Validate(&srv); err == nil || !strings.Contains(err.Error(), "auth.mode: none is only allowed in local mode") {
		t.Fatalf("want auth none error, got %v", err)
	}
	srv.Auth.Mode = "password"
	if err := Validate(&srv); err == nil || !strings.Contains(err.Error(), "is not none or oidc") {
		t.Fatalf("removed mode accepted: %v", err)
	}

	// An explicit mode from the environment wins over the derived default.
	cfg, err := Load(Sources{Env: []string{"PG_MODE=local", "PG_AUTH_MODE=oidc", "PG_AUTH_OIDC_ISSUER=http://127.0.0.1:5556/dex", "PG_AUTH_OIDC_CLIENT_ID=pg"}})
	if err != nil || cfg.Auth.Mode != protocol.AuthOIDC {
		t.Fatalf("local oidc: %v %+v", err, cfg)
	}
	if _, err := Load(Sources{Env: []string{"PG_MODE=server", "PG_AUTH_OIDC_ISSUER=not a url", "PG_AUTH_OIDC_CLIENT_ID=pg"}}); err == nil || !strings.Contains(err.Error(), "auth.oidc.issuer") {
		t.Fatalf("bad issuer: %v", err)
	}
}

func TestServerModeNeedsHTTPS(t *testing.T) {
	cfg := Defaults()
	cfg.Mode = protocol.ModeServer
	cfg.Auth.OIDC.ClientID = "pg"
	Resolve(&cfg)
	for _, tc := range []struct {
		publicURL, issuer string
		insecure          bool
		want              string // "" = valid
	}{
		{"https://emacs.example.edu", "https://idp.example.edu", false, ""},
		{"http://localhost:8080", "http://127.0.0.1:5556/dex", false, ""},
		{"http://[::1]:8080", "http://localhost/dex", false, ""},
		{"http://emacs.example.edu", "https://idp.example.edu", false, "publicUrl: \"http://emacs.example.edu\" must be https in server mode"},
		{"https://emacs.example.edu", "http://idp.example.edu", false, "auth.oidc.issuer: \"http://idp.example.edu\" must be https"},
		{"https://emacs.example.edu", "http://dex:5556/dex", true, ""},
	} {
		cfg.PublicURL, cfg.Auth.OIDC.Issuer, cfg.Auth.OIDC.AllowInsecureIssuer = tc.publicURL, tc.issuer, tc.insecure
		err := Validate(&cfg)
		if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s with issuer %s: want %q, got %v", tc.publicURL, tc.issuer, tc.want, err)
		}
	}
	// Local mode keeps any http publicUrl.
	local := Defaults()
	local.PublicURL = "http://emacs.lan:8080"
	Resolve(&local)
	if err := Validate(&local); err != nil {
		t.Fatal(err)
	}
}

func TestGroupLimits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	_ = os.WriteFile(p, []byte(`{"mode":"local","sessions":{"groupLimits":[{"group":"staff","maxActiveSessions":0},{"group":"cs101","maxActiveSessions":30}],"defaultGroupMaxActiveSessions":5}}`), 0o644)
	cfg, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.GroupLimit{{Group: "staff"}, {Group: "cs101", MaxActiveSessions: 30}}
	if !reflect.DeepEqual(cfg.Sessions.GroupLimits, want) || cfg.Sessions.DefaultGroupMaxActiveSessions != 5 {
		t.Fatalf("%+v", cfg.Sessions)
	}
	// The environment takes a JSON array.
	cfg, err = Load(Sources{Env: []string{`PG_SESSIONS_GROUP_LIMITS=[{"group":"cs101","maxActiveSessions":2}]`, "PG_SESSIONS_DEFAULT_GROUP_MAX_ACTIVE_SESSIONS=1"}})
	if err != nil || len(cfg.Sessions.GroupLimits) != 1 || cfg.Sessions.GroupLimits[0].MaxActiveSessions != 2 || cfg.Sessions.DefaultGroupMaxActiveSessions != 1 {
		t.Fatalf("env: %v %+v", err, cfg)
	}
	if _, err := Load(Sources{Env: []string{"PG_SESSIONS_GROUP_LIMITS=cs101"}}); err == nil {
		t.Fatal("non-JSON group limits accepted")
	}
	if _, err := Load(Sources{Env: []string{`PG_SESSIONS_GROUP_LIMITS=[{"group":"a","max":1}]`}}); err == nil {
		t.Fatal("unknown group limit key accepted")
	}
	bad := Defaults()
	bad.Sessions.GroupLimits = []protocol.GroupLimit{{Group: "a", MaxActiveSessions: 1}, {Group: "a"}, {MaxActiveSessions: -1}}
	bad.Sessions.DefaultGroupMaxActiveSessions = -1
	Resolve(&bad)
	err = Validate(&bad)
	for _, want := range []string{"listed twice", "groupLimits[2].group: required", "groupLimits[2].maxActiveSessions", "defaultGroupMaxActiveSessions"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}

func TestValidate(t *testing.T) {
	cfg := Defaults()
	cfg.Mode = protocol.ModeServer
	cfg.Auth.OIDC.Issuer, cfg.Auth.OIDC.ClientID = "https://idp.example.edu", "pg"
	Resolve(&cfg)
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Sessions.Limits.Memory = "lots"
	cfg.Sessions.GUI.Geometry = "big"
	err := Validate(&cfg)
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
		"sessions.groupLimits":          "PG_SESSIONS_GROUP_LIMITS",
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
