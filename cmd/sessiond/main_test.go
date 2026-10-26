package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthAddr(t *testing.T) {
	for in, want := range map[string]string{
		":7000":          "127.0.0.1:7000",
		"0.0.0.0:7000":   "127.0.0.1:7000",
		"127.0.0.1:7000": "127.0.0.1:7000",
		"[::]:7000":      "127.0.0.1:7000",
		"10.0.0.2:7000":  "10.0.0.2:7000",
	} {
		if got := healthAddr(in); got != want {
			t.Errorf("healthAddr(%q) = %q want %q", in, got, want)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	ok := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(404)
			return
		}
		if !ok {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"ok":false,"reason":"Emacs is starting"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := healthcheck(addr, time.Second); err != nil {
		t.Fatal(err)
	}
	ok = false
	if err := healthcheck(addr, time.Second); err == nil || !strings.Contains(err.Error(), "Emacs is starting") {
		t.Fatalf("want failure with reason, got %v", err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	if err := healthcheck(dead, time.Second); err == nil {
		t.Fatal("dead address should fail")
	}

	// Through the CLI: -listen overrides PG_LISTEN.
	var out, errb bytes.Buffer
	ok = true
	if code := run([]string{"-listen", addr, "healthcheck"}, []string{"PG_LISTEN=:1"}, &out, &errb); code != 0 {
		t.Fatalf("healthcheck exit %d: %s", code, errb.String())
	}
}

func TestConfigFlagsAndDefaults(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"-workspace", "/home/x/work", "-gatewayUrl", "", "config"},
		[]string{"PG_RUN_DIR=/home/x/run", "PG_SESSION_TOKEN=secret", "PG_GATEWAY_URL=http://gw:8080"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var cfg map[string]any
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["workspace"] != "/home/x/work" || cfg["gatewayUrl"] != "" || cfg["agentSocket"] != "/home/x/run/agent.sock" {
		t.Fatalf("config %v", cfg)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("token not redacted")
	}
	if code := run([]string{"version"}, nil, &out, &errb); code != 0 {
		t.Fatal("version")
	}
	if code := run([]string{"bogus"}, nil, &out, &errb); code != 2 {
		t.Fatal("unknown command")
	}
	if code := run([]string{"config"}, []string{"PG_LOG_LEVEL=loud"}, &out, &errb); code != 1 {
		t.Fatal("invalid config should fail")
	}
}
