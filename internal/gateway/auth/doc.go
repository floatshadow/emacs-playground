// Package auth authenticates learners (backend.md §2.6): auth mode none
// (single local user), invite codes (constant-time compare, use counting,
// expiry, hashed at rest), OIDC (github.com/coreos/go-oidc/v3 with PKCE),
// HMAC-signed pg_session cookies and the Origin check for state-changing
// requests.
//
// Interface (backend.md §2.3):
//
//	type Authenticator interface {
//	    Middleware(next http.Handler) http.Handler // sets the user or 401s
//	    Routes(mux *http.ServeMux)                 // login, callback, logout
//	}
//
// TODO(gateway, backend.md §2.6): none and invite in slice 1; OIDC routes
// return 501 not_implemented until implemented.
package auth
