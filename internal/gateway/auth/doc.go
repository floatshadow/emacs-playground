// Package auth authenticates learners (backend.md §2.6, deployment.md
// §9.3).
//
// Authenticator (backend.md §2.3) has three implementations chosen by
// auth.mode:
//
//   - none (local mode only): every request is the single user "local",
//     stored in the store on first use.
//   - invite: POST /api/v1/auth/invite {code} hashes the code (SHA-256; only
//     hashes are stored), redeems it with use counting and expiry
//     (store.InviteRepo.Redeem, constant-time comparison), creates a user
//     with a random id and the code's label as display name, and sets the
//     pg_session cookie. A browser that is already signed in does not use
//     up another use. Attempts are rate limited per client IP and per code.
//   - oidc: cookies are verified as for invites, but the login routes
//     (GET /auth/oidc/start and /callback) answer 501 not_implemented in
//     slice 1. TODO(gateway, backend.md §2.6): go-oidc with PKCE.
//
// The pg_session cookie value is "<base64url user id>.<expiry unix
// seconds>.<base64url HMAC-SHA256>" keyed by auth.cookieSecretFile
// (generated on first start), HttpOnly, SameSite=Lax, Secure when the
// public URL is https, Path=/, auth.sessionDays long and renewed on use at
// most daily. The Origin check (CSRF) is the gateway's middleware
// (httpx.OriginCheck).
//
// The invites file (deploy/invites.example.txt) has one code per line:
// "<code> [label] [uses=N] [until=YYYY-MM-DD]"; until is inclusive (UTC).
// It is synced into the store at start and on SIGHUP: new codes are added,
// limits updated, use counts kept, removed codes deleted.
package auth
