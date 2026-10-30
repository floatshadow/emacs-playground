// Package auth authenticates learners (backend.md §2.6, deployment.md
// §9.3).
//
// Authenticator (backend.md §2.3) has two implementations chosen by
// auth.mode:
//
//   - none (local mode only): every request is the single user "local",
//     stored in the store on first use.
//   - oidc (the server-mode default): OpenID Connect against any provider
//     with discovery. GET /api/v1/auth/oidc/start[?next=/path] discovers
//     the provider from auth.oidc.issuer (one discovery shared by
//     concurrent callers, cached after the first success; a failure is
//     cached for 15 s), stores state, nonce, PKCE verifier and next
//     in the pg_oidc cookie (signed with a key derived from the cookie
//     secret, 10 minutes, Path /api/v1/auth/oidc/) and redirects to the
//     authorization endpoint with code_challenge_method S256. The callback
//     checks the cookie and state, exchanges the code with the verifier
//     (client secret from auth.oidc.clientSecretFile, or none for a public
//     client), verifies the ID token with go-oidc (signature, issuer,
//     audience = clientId, expiry) and its nonce, applies
//     allowedEmailDomains (an email with email_verified true is required)
//     and allowedGroups, upserts the user by issuer + "|" + sub
//     (id UserID(subject); display name, username, email and groups
//     refreshed at every login), sets pg_session and redirects to next. A
//     failure redirects to /login?error=<protocol.LoginError>. POST
//     /api/v1/auth/logout revokes the user's cookies and, with
//     auth.oidc.providerLogout and an advertised end_session_endpoint,
//     answers 200 {redirectUrl} for RP-initiated logout (client_id and
//     post_logout_redirect_uri <publicUrl>/login); otherwise 204.
//
// The pg_session cookie value is "<base64url user id|session epoch>.<expiry
// unix seconds>.<base64url HMAC-SHA256>" keyed by auth.cookieSecretFile
// (generated on first start), HttpOnly, SameSite=Lax, Secure when the
// public URL is https, Path=/, valid auth.sessionDays from the sign-in and
// never extended, so learners return to the provider at least that often.
// Logout increments the stored epoch, which revokes every cookie of the
// user. Each request rechecks the stored user against the allow-lists, and
// only users of the active mode's kind are accepted. The
// Origin check (CSRF) is the gateway's middleware (httpx.OriginCheck).
package auth
