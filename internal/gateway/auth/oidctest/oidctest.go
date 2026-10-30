// Package oidctest is a fake OpenID Connect provider for tests: discovery,
// JWKS, an authorization endpoint that signs the configured user in at
// once, and a token endpoint that checks PKCE and returns an RS256 ID
// token.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Provider is a running fake provider.
type Provider struct {
	Server   *httptest.Server
	Issuer   string
	ClientID string
	// ClientSecret, when set, is required at the token endpoint (Basic or
	// form); empty makes a public client.
	ClientSecret string

	key *rsa.PrivateKey

	mu sync.Mutex
	// Claims are added to the next ID tokens (sub, email, name, groups…).
	Claims map[string]any
	// Tamper edits the ID token claims before signing.
	Tamper func(claims map[string]any)
	// Deny makes the authorization endpoint answer error=access_denied.
	Deny bool
	// EndSession advertises an end_session_endpoint.
	EndSession bool
	// Now is the clock for iat and exp, default time.Now.
	Now    func() time.Time
	codes  map[string]grant
	Tokens int // successful token requests
}

type grant struct {
	nonce, challenge, redirectURI string
	claims                        map[string]any
}

// New starts a provider; Close it with p.Server.Close (or t.Cleanup).
func New(clientID, clientSecret string) *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p := &Provider{ClientID: clientID, ClientSecret: clientSecret, key: key, codes: map[string]grant{}, Now: time.Now,
		Claims: map[string]any{"sub": "42"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	p.Server = httptest.NewServer(mux)
	p.Issuer = p.Server.URL
	return p
}

// SetClaims replaces the claims of the next logins.
func (p *Provider) SetClaims(c map[string]any) {
	p.mu.Lock()
	p.Claims = c
	p.mu.Unlock()
}

// SetTamper sets Tamper.
func (p *Provider) SetTamper(f func(map[string]any)) {
	p.mu.Lock()
	p.Tamper = f
	p.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	doc := map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/authorize",
		"token_endpoint":                        p.Issuer + "/token",
		"jwks_uri":                              p.Issuer + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	if p.EndSession {
		doc["end_session_endpoint"] = p.Issuer + "/logout?realm=main"
	}
	writeJSON(w, 200, doc)
}

var b64 = base64.RawURLEncoding

func (p *Provider) keys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
		"n": b64.EncodeToString(p.key.N.Bytes()),
		"e": b64.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b64.EncodeToString(b)
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Host == "" {
		http.Error(w, "bad redirect_uri", 400)
		return
	}
	switch {
	case q.Get("client_id") != p.ClientID, q.Get("response_type") != "code",
		!slices.Contains(strings.Fields(q.Get("scope")), "openid"),
		q.Get("code_challenge_method") != "S256", q.Get("code_challenge") == "", q.Get("state") == "":
		http.Error(w, "bad authorization request", 400)
		return
	}
	v := redirect.Query()
	v.Set("state", q.Get("state"))
	p.mu.Lock()
	if p.Deny {
		v.Set("error", "access_denied")
	} else {
		code := random()
		p.codes[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), claims: maps.Clone(p.Claims)}
		v.Set("code", code)
	}
	p.mu.Unlock()
	redirect.RawQuery = v.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != p.ClientID || (p.ClientSecret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(p.ClientSecret)) != 1) {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirectURI ||
		b64.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	now := p.Now()
	claims := map[string]any{"iss": p.Issuer, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
	if g.nonce != "" {
		claims["nonce"] = g.nonce
	}
	maps.Copy(claims, g.claims)
	if p.Tamper != nil {
		p.Tamper(claims)
	}
	idToken, err := p.sign(claims)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "server_error"})
		return
	}
	p.Tokens++
	writeJSON(w, 200, map[string]any{"access_token": random(), "token_type": "Bearer", "expires_in": 300, "id_token": idToken})
}

func (p *Provider) sign(claims map[string]any) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + b64.EncodeToString(sig), nil
}

// Authorize performs the browser's visit to an authorization URL and
// returns the redirect back to the client (the callback URL with code and
// state, or error).
func (p *Provider) Authorize(authURL string) (string, error) {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authURL)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return "", errors.New("oidctest: authorize answered " + resp.Status)
	}
	return resp.Header.Get("Location"), nil
}
