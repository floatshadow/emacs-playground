package protocol

// Deployment configuration (deployment.md §3). The gateway reads one JSON
// file (deploy/config.json, mounted at /etc/playground/config.json, path
// overridable with PG_CONFIG). internal/config applies defaults, the file,
// PG_* environment overrides and flags, then validates. JSON Schema
// (deploy/config.schema.json) is generated from these structs by
// `playctl config schema`.
//
// Fields tagged secret:"true" are redacted when the effective configuration
// is logged.

// DeployMode is local (one fixed session) or server (orchestrator).
type DeployMode string

// Deployment modes.
const (
	ModeLocal  DeployMode = "local"
	ModeServer DeployMode = "server"
)

// AuthMode selects learner authentication.
type AuthMode string

// Auth modes. none is only valid in local mode.
const (
	AuthNone   AuthMode = "none"
	AuthInvite AuthMode = "invite"
	AuthOIDC   AuthMode = "oidc"
)

// SessionBackend selects how sessions are provided.
type SessionBackend string

// Session backends.
const (
	BackendStatic SessionBackend = "static"
	BackendDocker SessionBackend = "docker"
)

// EgressPolicy controls session internet access (deployment.md §9.2).
type EgressPolicy string

// Egress policies.
const (
	EgressNone       EgressPolicy = "none"
	EgressRegistries EgressPolicy = "registries"
	EgressAll        EgressPolicy = "all"
)

// Config is the gateway configuration file.
type Config struct {
	// Schema is the optional "$schema" reference; ignored.
	Schema string `json:"$schema,omitempty"`
	// Mode is local (default) or server.
	Mode DeployMode `json:"mode" jsonschema:"enum=local,enum=server"`
	// Listen is the gateway listen address, default ":8080".
	Listen string `json:"listen"`
	// PublicURL is used for cookies, OIDC redirects and Origin checks;
	// default "http://localhost:8080".
	PublicURL string `json:"publicUrl"`
	// DataDir holds the SQLite database, the local progress file, the
	// curriculum cache and the cookie secret; default "/data".
	DataDir    string           `json:"dataDir"`
	Auth       AuthConfig       `json:"auth"`
	Sessions   SessionsConfig   `json:"sessions"`
	Local      LocalConfig      `json:"local"`
	Curriculum CurriculumConfig `json:"curriculum"`
	Logging    LoggingConfig    `json:"logging"`
	Metrics    MetricsConfig    `json:"metrics"`
	Features   FeaturesConfig   `json:"features"`
}

// AuthConfig configures authentication.
type AuthConfig struct {
	// Mode is none (local only, default), invite or oidc.
	Mode    AuthMode      `json:"mode" jsonschema:"enum=none,enum=invite,enum=oidc"`
	Invites InvitesConfig `json:"invites"`
	OIDC    OIDCConfig    `json:"oidc"`
	// CookieSecretFile holds the HMAC key; generated on first start if
	// missing; default "<dataDir>/cookie.secret".
	CookieSecretFile string `json:"cookieSecretFile"`
	// SessionDays is the cookie lifetime, default 30.
	SessionDays int `json:"sessionDays"`
}

// InvitesConfig configures invite codes.
type InvitesConfig struct {
	// File has one code per line with optional label, uses=N and
	// until=YYYY-MM-DD (deploy/invites.example.txt).
	File string `json:"file"`
}

// OIDCConfig configures OpenID Connect login.
type OIDCConfig struct {
	Issuer           string `json:"issuer"`
	ClientID         string `json:"clientId"`
	ClientSecretFile string `json:"clientSecretFile"`
	// AllowedEmailDomains restricts logins; empty allows any.
	AllowedEmailDomains []string `json:"allowedEmailDomains"`
}

// SessionsConfig configures session provisioning.
type SessionsConfig struct {
	// Backend is static (default in local mode) or docker (default in server
	// mode).
	Backend SessionBackend `json:"backend" jsonschema:"enum=static,enum=docker"`
	// DockerHost is the Engine API endpoint, normally the socket proxy
	// ("tcp://socket-proxy:2375"); empty uses DOCKER_HOST.
	DockerHost string `json:"dockerHost"`
	// Image is the session image.
	Image string `json:"image"`
	// Network is the Docker network for session containers.
	Network string `json:"network"`
	// Runtime is runc (default) or runsc (gVisor).
	Runtime string       `json:"runtime"`
	Limits  LimitsConfig `json:"limits"`
	// IdleStopMinutes stops a session with no WebSocket for this long,
	// default 30.
	IdleStopMinutes int `json:"idleStopMinutes"`
	// MaxSessions refuses new sessions beyond this, default 50.
	MaxSessions int `json:"maxSessions"`
	// VolumeRetentionDays removes workspace volumes unused for this long,
	// default 30.
	VolumeRetentionDays int `json:"volumeRetentionDays"`
	// Egress is none, registries (default) or all.
	Egress    EgressPolicy    `json:"egress" jsonschema:"enum=none,enum=registries,enum=all"`
	RemoteBox RemoteBoxConfig `json:"remoteBox"`
	GUI       GUIConfig       `json:"gui"`
	Terminal  TerminalConfig  `json:"terminal"`
}

// LimitsConfig are per-session resource limits.
type LimitsConfig struct {
	// CPUs, default 2.
	CPUs float64 `json:"cpus"`
	// Memory, default "3g" (Docker size syntax: b, k, m, g).
	Memory string `json:"memory"`
	// Pids, default 1024.
	Pids int64 `json:"pids"`
	// Disk is a volume size hint, default "4g"; enforced only on storage
	// drivers with quotas.
	Disk string `json:"disk"`
}

// RemoteBoxConfig configures the per-learner SSH box.
type RemoteBoxConfig struct {
	// Enabled provisions an SSH box per learner, default true.
	Enabled bool   `json:"enabled"`
	Image   string `json:"image"`
}

// GUIConfig configures the GUI frame.
type GUIConfig struct {
	// Enabled makes the GUI frame available, default true.
	Enabled bool `json:"enabled"`
	// Geometry is the Xvnc screen size, default "1600x900".
	Geometry string `json:"geometry"`
}

// TerminalConfig configures the terminal frame.
type TerminalConfig struct {
	// InitialSize is "COLSxROWS" until the browser resizes, default
	// "120x36".
	InitialSize string `json:"initialSize"`
}

// LocalConfig configures local mode.
type LocalConfig struct {
	// SessionURL is the fixed session's sessiond, default
	// "http://session:7000".
	SessionURL string `json:"sessionUrl"`
	// Workspace is documented for the Makefile; Compose mounts it.
	Workspace string `json:"workspace"`
	// SessionID is the fixed session's id (PG_SESSION_ID of the session
	// container), default "local". Extension over deployment.md §3.
	SessionID string `json:"sessionId"`
	// SessionToken is the fixed session's bearer token for /internal
	// (PG_SESSION_TOKEN of the session container), default "local".
	// Extension over deployment.md §3.
	SessionToken string `json:"sessionToken" secret:"true"`
}

// CurriculumConfig locates the built curriculum bundle.
type CurriculumConfig struct {
	// Dir holds curriculum.json, lesson bundles and fixture tarballs,
	// default "/srv/curriculum".
	Dir string `json:"dir"`
	// DefaultLocale, default "en".
	DefaultLocale string `json:"defaultLocale"`
}

// LoggingConfig configures slog.
type LoggingConfig struct {
	// Level is debug, info (default), warn or error.
	Level string `json:"level" jsonschema:"enum=debug,enum=info,enum=warn,enum=error"`
	// Format is json (default) or text.
	Format string `json:"format" jsonschema:"enum=json,enum=text"`
}

// MetricsConfig configures the optional Prometheus endpoint.
type MetricsConfig struct {
	// Enabled, default false.
	Enabled bool `json:"enabled"`
	// Listen, default ":9090".
	Listen string `json:"listen"`
}

// FeaturesConfig toggles optional features.
type FeaturesConfig struct {
	// StuckReports allows "I'm stuck" reports, default true.
	StuckReports bool `json:"stuckReports"`
	// TelemetryUpload is never on by default.
	TelemetryUpload bool `json:"telemetryUpload"`
	// AuthorMode shows condition results and raw state in lessons and
	// enables rpc eval through the gateway, default false.
	AuthorMode bool `json:"authorMode"`
}

// SessiondConfig is sessiond's configuration, read only from the
// environment (deployment.md §3); the env tag names the variable and the
// default tag its default. Flags with the same names in lower case
// (-listen, -workspace) override for development.
type SessiondConfig struct {
	SessionID    string `json:"sessionId" env:"PG_SESSION_ID" default:"local"`
	SessionToken string `json:"sessionToken" env:"PG_SESSION_TOKEN" default:"local" secret:"true"`
	// GatewayURL is where sessiond fetches fixtures and reports progress.
	GatewayURL string `json:"gatewayUrl" env:"PG_GATEWAY_URL" default:"http://gateway:8080"`
	Listen     string `json:"listen" env:"PG_LISTEN" default:":7000"`
	Workspace  string `json:"workspace" env:"PG_WORKSPACE" default:"/work"`
	// RunDir holds the agent socket and the Emacs server socket directory.
	RunDir      string `json:"runDir" env:"PG_RUN_DIR" default:"/run/playground"`
	AgentSocket string `json:"agentSocket" env:"PG_AGENT_SOCKET" default:"/run/playground/agent.sock"`
	VNCGeometry string `json:"vncGeometry" env:"PG_VNC_GEOMETRY" default:"1600x900"`
	TermSize    string `json:"termSize" env:"PG_TERM_SIZE" default:"120x36"`
	// PristineConfig is the read-only config copied to /work/emacs.d on
	// first start.
	PristineConfig string `json:"pristineConfig" env:"PG_PRISTINE_CONFIG" default:"/opt/playground/emacs.d"`
	// EmacsServer is the daemon name (emacs --fg-daemon=NAME).
	EmacsServer string `json:"emacsServer" env:"PG_EMACS_SERVER" default:"playground"`
	// TmuxSocket is the tmux -L socket name.
	TmuxSocket string `json:"tmuxSocket" env:"PG_TMUX_SOCKET" default:"pg"`
	// Display is the Xvnc display for the GUI frame.
	Display string `json:"display" env:"PG_DISPLAY" default:":1"`
	VNCPort int    `json:"vncPort" env:"PG_VNC_PORT" default:"5901"`
	// GUIEnabled allows the GUI frame.
	GUIEnabled bool `json:"guiEnabled" env:"PG_GUI_ENABLED" default:"true"`
	// Binaries, overridable for tests.
	EmacsBin       string `json:"emacsBin" env:"PG_EMACS_BIN" default:"emacs"`
	EmacsclientBin string `json:"emacsclientBin" env:"PG_EMACSCLIENT_BIN" default:"emacsclient"`
	TmuxBin        string `json:"tmuxBin" env:"PG_TMUX_BIN" default:"tmux"`
	XvncBin        string `json:"xvncBin" env:"PG_XVNC_BIN" default:"Xvnc"`
	XdotoolBin     string `json:"xdotoolBin" env:"PG_XDOTOOL_BIN" default:"xdotool"`
	GitBin         string `json:"gitBin" env:"PG_GIT_BIN" default:"git"`
	// VersionsFile is images/session/versions.env as installed.
	VersionsFile string `json:"versionsFile" env:"PG_VERSIONS_FILE" default:"/opt/playground/versions.env"`
	LogLevel     string `json:"logLevel" env:"PG_LOG_LEVEL" default:"info"`
	LogFormat    string `json:"logFormat" env:"PG_LOG_FORMAT" default:"json"`
	// AuthorMode allows rpc eval (the gateway also gates it).
	AuthorMode bool `json:"authorMode" env:"PG_AUTHOR_MODE" default:"false"`
}
