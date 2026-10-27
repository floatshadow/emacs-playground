package config

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// LoadSessiond reads sessiond's configuration from env (KEY=VALUE pairs)
// using the env and default tags of protocol.SessiondConfig.
func LoadSessiond(env []string) (*protocol.SessiondConfig, error) {
	m := envMap(env)
	cfg := &protocol.SessiondConfig{}
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("env")
		if name == "" {
			continue
		}
		val, ok := m[name]
		if !ok {
			val = f.Tag.Get("default")
		}
		if err := setValue(v.Field(i), name, val); err != nil {
			return nil, err
		}
	}
	if _, _, err := ParseGeometry(cfg.VNCGeometry); err != nil {
		return nil, fmt.Errorf("PG_VNC_GEOMETRY: %w", err)
	}
	if _, _, err := ParseGeometry(cfg.TermSize); err != nil {
		return nil, fmt.Errorf("PG_TERM_SIZE: %w", err)
	}
	if cfg.VNCPort <= 0 || cfg.VNCPort > 65535 {
		return nil, fmt.Errorf("PG_VNC_PORT: %d out of range", cfg.VNCPort)
	}
	if !levels[cfg.LogLevel] {
		return nil, fmt.Errorf("PG_LOG_LEVEL: %q is not debug, info, warn or error", cfg.LogLevel)
	}
	if !formats[cfg.LogFormat] {
		return nil, fmt.Errorf("PG_LOG_FORMAT: %q is not json or text", cfg.LogFormat)
	}
	return cfg, nil
}

// SessiondEnv renders cfg back to environment variables (the gateway uses
// it to build session container environments).
func SessiondEnv(cfg *protocol.SessiondConfig) []string {
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

// RedactedSessiond returns sessiond's configuration with secrets redacted.
func RedactedSessiond(cfg *protocol.SessiondConfig) map[string]any {
	return redact(reflect.ValueOf(*cfg))
}
