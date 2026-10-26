package gateway

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

// placeholderHTML is served when the binary has no web bundle (built
// without -tags embedweb and no -web directory): the API works, the app
// is elsewhere.
const placeholderHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Emacs Playground</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font:16px/1.5 Georgia,serif;max-width:40rem;margin:4rem auto;padding:0 1rem;color:#222}code{background:#eee;padding:0 .3em}</style>
</head><body>
<h1>Emacs Playground gateway</h1>
<p>This gateway was built without the web app, so there is nothing to show here yet. The API is running.</p>
<ul>
<li>Development: run <code>make dev</code> and open <a href="http://localhost:5173">http://localhost:5173</a>.</li>
<li>Embedded app: <code>make gateway</code> (builds <code>web/dist</code>, then <code>go build -tags embedweb ./cmd/gateway</code>).</li>
<li>From disk: start the gateway with <code>-web web/dist</code>.</li>
</ul>
</body></html>
`

// securityHeaders are set on app pages (deployment.md §9.3). They are not
// set on /s/ (previews are framed by the app and carry sessiond's own
// sandboxing CSP).
func (a *App) securityHeaders(h http.Header) {
	wsOrigins := ""
	if u, err := url.Parse(a.cfg.PublicURL); err == nil && u.Host != "" {
		scheme := "ws"
		if u.Scheme == "https" {
			scheme = "wss"
		}
		wsOrigins = " " + scheme + "://" + u.Host
	}
	h.Set("Content-Security-Policy", "default-src 'self'; "+
		"script-src 'self' 'wasm-unsafe-eval'; "+
		"style-src 'self' 'unsafe-inline'; "+
		"img-src 'self' data: blob:; "+
		"font-src 'self' data:; "+
		"connect-src 'self'"+wsOrigins+"; "+
		"worker-src 'self' blob:; "+
		"frame-src 'self'; "+
		"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
}

// webApp serves the bundle with SPA fallback: existing files as they are,
// paths without a file extension get index.html, missing files with an
// extension get 404.
func (a *App) webApp() http.Handler {
	dist := a.o.Web
	if dist != nil {
		if _, err := fs.Stat(dist, "index.html"); err != nil {
			a.log.Warn("gateway: the web bundle has no index.html; serving the placeholder")
			dist = nil
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			httpx.WriteError(w, r, &httpx.Error{Status: http.StatusMethodNotAllowed, Code: protocol.ErrInvalidRequest, Message: "method not allowed"})
			return
		}
		a.securityHeaders(w.Header())
		if dist == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(placeholderHTML))
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(dist, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite content-hashes everything under assets/.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, dist, name)
			return
		}
		if path.Ext(name) != "" && name != "index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, dist, "index.html")
	})
}

// fonts serves /fonts/{file...} (frontend.md §5).
func (a *App) fonts() http.Handler {
	fonts := a.o.Fonts
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.PathValue("file"))
		if fonts == nil || name == "." || strings.HasPrefix(name, "..") {
			http.NotFound(w, r)
			return
		}
		st, err := fs.Stat(fonts, name)
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch path.Ext(name) {
		case ".woff2":
			w.Header().Set("Content-Type", "font/woff2")
		case ".woff":
			w.Header().Set("Content-Type", "font/woff")
		case ".ttf":
			w.Header().Set("Content-Type", "font/ttf")
		case ".otf":
			w.Header().Set("Content-Type", "font/otf")
		}
		http.ServeFileFS(w, r, fonts, name)
	})
}

// FontsDir finds the fonts directory: explicit, else the first existing of
// /srv/fonts (gateway image), .dev/fonts (`playctl fonts` in a checkout)
// and web/public/fonts, else the bundle's fonts/ directory. It returns nil
// when none exists.
func FontsDir(explicit string, web fs.FS) fs.FS {
	if explicit != "" {
		return os.DirFS(explicit)
	}
	for _, d := range []string{"/srv/fonts", ".dev/fonts", "web/public/fonts"} {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return os.DirFS(d)
		}
	}
	if web != nil {
		if sub, err := fs.Sub(web, "fonts"); err == nil {
			if _, err := fs.Stat(sub, "."); err == nil {
				return sub
			}
		}
	}
	return nil
}

// WebDir opens a web bundle directory from disk (-web flag); nil when the
// directory does not exist.
func WebDir(dir string) (fs.FS, error) {
	st, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("gateway: -web " + dir + " is not a directory")
	}
	return os.DirFS(dir), nil
}

// metricsHandler serves Prometheus metrics (backend.md §5, optional):
// sessions by state, open WebSockets, curriculum loaded, Go and process
// metrics. TODO(gateway, backend.md §5): session creation duration and
// proxy latency histograms.
func (a *App) metricsHandler() http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(&appCollector{a: a})
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

type appCollector struct{ a *App }

var (
	descSessions   = prometheus.NewDesc("playground_sessions", "Sessions by state.", []string{"state"}, nil)
	descWebSockets = prometheus.NewDesc("playground_websockets", "Open proxied WebSockets.", nil, nil)
	descCurriculum = prometheus.NewDesc("playground_curriculum_loaded", "1 when a curriculum bundle is served.", []string{"version"}, nil)
)

func (c *appCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descSessions
	ch <- descWebSockets
	ch <- descCurriculum
}

func (c *appCollector) Collect(ch chan<- prometheus.Metric) {
	counts, ws := c.a.Sessions.Counts()
	for _, st := range []protocol.SessionState{protocol.SessionCreating, protocol.SessionStarting, protocol.SessionReady,
		protocol.SessionIdle, protocol.SessionStopping, protocol.SessionStopped, protocol.SessionFailed, protocol.SessionRemoved} {
		ch <- prometheus.MustNewConstMetric(descSessions, prometheus.GaugeValue, float64(counts[st]), string(st))
	}
	ch <- prometheus.MustNewConstMetric(descWebSockets, prometheus.GaugeValue, float64(ws))
	loaded, version := c.a.Curriculum.Loaded()
	v := 0.0
	if loaded {
		v = 1
	}
	ch <- prometheus.MustNewConstMetric(descCurriculum, prometheus.GaugeValue, v, version)
}
