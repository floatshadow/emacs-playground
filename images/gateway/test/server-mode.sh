#!/bin/sh
# Server-mode end-to-end test of the gateway image against a real Docker
# Engine and a real OpenID Connect provider. It runs the server stack
# (compose.yaml + compose.server.yaml: Caddy, the socket proxy, the gateway)
# in its own Compose project on high ports, with Dex as the provider (static
# client, static users in groups) and a stand-in session image
# (fake-session/: /healthz, /status, /rpc/*, a heartbeat with its token,
# answers only with its proxy secret). It checks: the authorization code
# flow through Dex (curl submits Dex's login form), /me groups, refusals by
# email domain and group, the group quota (429 group_quota_exceeded, freed
# when a session stops), the default group pool, session creation with the
# hardening profile, the session's own internal network (a container on the
# shared network cannot reach it, one on its network gets 401 without the
# proxy secret), the proxy, the /internal heartbeat, stop (container and
# network removed, volume kept), re-creation, the registry rebuild after the
# gateway container is replaced (it joins the session's network again),
# and logout (which revokes the cookie, even a copy kept elsewhere).
# Everything it creates is removed at the end.
#
#   images/gateway/test/server-mode.sh [gateway image]
#
# Run from the repository root: the work directory (artifacts/) is
# bind-mounted, so it must be on a path the Docker daemon can see.
set -eu
image=${1:-ghcr.io/floatshadow/emacs-playground/gateway:dev}
dex_image=ghcr.io/dexidp/dex:v2.46.1
port=${PG_IT_PORT:-18443}
dex_port=${PG_IT_DEX_PORT:-15556}
base=https://localhost:$port
origin="Origin: $base"
# Dex over http on the compose network, hence allowInsecureIssuer below.
issuer=http://dex:5556/dex
project=pg-it-$$
net=${project}_sessions
work=$(pwd)/artifacts/gateway-it-$$
mkdir -p "$work"
: > "$work/users"

# The learners' containers, networks and volumes (labelled with their user
# id) go after the gateway, which is attached to their networks.
cleanup() {
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  while read -r uid; do
    f=label=playground.user=$uid
    docker ps -aq --filter "$f" | xargs -r docker rm -f >/dev/null 2>&1 || true
    docker network ls -q --filter "$f" | xargs -r docker network rm >/dev/null 2>&1 || true
    docker volume ls -q --filter "$f" | xargs -r docker volume rm >/dev/null 2>&1 || true
  done < "$work/users"
  rm -rf "$work"
}
compose() {
  docker compose -p "$project" --env-file "$work/it.env" \
    -f compose.yaml -f compose.server.yaml -f "$work/compose.it.yaml" "$@"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM PIPE
fail() {
  echo "FAIL: $*" >&2
  compose logs --no-color --tail 30 gateway dex >&2 || true
  exit 1
}
docker image inspect "$image" >/dev/null 2>&1 || { echo "no image $image" >&2; exit 1; }
docker build -q -t pg-fake-session:it "$(dirname "$0")/fake-session" >/dev/null

# ---- configuration -----------------------------------------------------------
# Users (password "password"): alice and amy in it-a (limit 1), bob and
# brian in it-b (the default pool, 1), mallory in it-a with a domain that is
# not allowed, nora in no allowed group. User ids (and so workspace volume
# names) are unique per run.
# shellcheck disable=SC2016 # bcrypt of "password", not an expansion
hash='$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W'
user() { # name domain group
  printf '  - {email: "%s@%s", hash: "%s", username: "%s", preferredUsername: "%s", userID: "%s-%s", groups: ["%s"]}\n' \
    "$1" "$2" "$hash" "$1" "$1" "$1" "$$" "$3"
}
{
  cat <<EOF
issuer: $issuer
storage: {type: memory}
web: {http: 0.0.0.0:5556}
oauth2: {skipApprovalScreen: true}
enablePasswordDB: true
staticClients:
  - id: emacs-playground
    name: Emacs Playground
    secret: it-client-secret
    redirectURIs: ["$base/api/v1/auth/oidc/callback"]
staticPasswords:
EOF
  user alice example.com it-a
  user amy example.com it-a
  user bob example.com it-b
  user brian example.com it-b
  user mallory example.org it-a
  user nora example.com it-other
} > "$work/dex.yaml"
cat > "$work/config.json" <<EOF
{
  "mode": "server",
  "publicUrl": "$base",
  "auth": {
    "oidc": {
      "issuer": "$issuer",
      "allowInsecureIssuer": true,
      "clientId": "emacs-playground",
      "clientSecretFile": "/run/secrets/oidc_client_secret",
      "scopes": ["openid", "email", "profile", "groups"],
      "displayName": "Dex",
      "allowedEmailDomains": ["example.com"],
      "allowedGroups": ["it-a", "it-b"]
    }
  },
  "sessions": {
    "dockerHost": "tcp://socket-proxy:2375",
    "image": "pg-fake-session:it",
    "network": "$net",
    "limits": { "memory": "256m", "pids": 256 },
    "remoteBox": { "enabled": false }
  },
  "logging": { "format": "text" }
}
EOF
# The client secret as `make server` lays it out: 644 in a 700 directory.
mkdir -m 700 "$work/secrets"
printf 'it-client-secret\n' > "$work/secrets/oidc_client_secret"
printf 'DOMAIN=localhost\n' > "$work/it.env"
# The group limits come from PG_* variables (JSON for the list), the rest from
# the file.
cat > "$work/compose.it.yaml" <<EOF
services:
  caddy:
    ports: !override ["127.0.0.1:$port:443"]
  gateway:
    image: $image
    environment:
      PG_SESSIONS_GROUP_LIMITS: '[{"group":"it-a","maxActiveSessions":1}]'
      PG_SESSIONS_DEFAULT_GROUP_MAX_ACTIVE_SESSIONS: "1"
    volumes:
      - $work/config.json:/etc/playground/config.json:ro
    depends_on: [socket-proxy, dex]
  dex:
    image: $dex_image
    command: [dex, serve, /etc/dex/config.yaml]
    volumes:
      - $work/dex.yaml:/etc/dex/config.yaml:ro
    ports: ["127.0.0.1:$dex_port:5556"]
    networks: [front]
secrets:
  oidc_client_secret:
    file: $work/secrets/oidc_client_secret
EOF
chmod 644 "$work/dex.yaml" "$work/config.json" "$work/secrets/oidc_client_secret"

# ---- helpers -----------------------------------------------------------------
# req USER ARGS...: curl as USER (own cookie jar) against the stack; the
# provider's host name resolves to its published port.
req() {
  u=$1
  shift
  curl -sk --connect-to "dex:5556:127.0.0.1:$dex_port" -c "$work/$u.jar" -b "$work/$u.jar" "$@"
}
# login USER: the authorization code flow; prints where the browser ends up.
login() {
  page=$(req "$1" -L -w '\n%{url_effective}' "$base/api/v1/auth/oidc/start?next=/learn")
  at=$(echo "$page" | tail -n 1)
  action=$(echo "$page" | sed -n 's/.*<form method="post" action="\([^"]*\)".*/\1/p' | sed 's/&amp;/\&/g')
  [ -n "$action" ] || fail "no Dex login form at $at"
  case "$action" in /*) action=http://dex:5556$action ;; esac
  email=$(sed -n "s/.*email: \"\($1@[^\"]*\)\".*/\1/p" "$work/dex.yaml")
  req "$1" -L -o /dev/null -w '%{url_effective}' \
    --data-urlencode "login=$email" --data-urlencode password=password "$action"
  req "$1" "$base/api/v1/me" | json 'j["user"]["id"]' | grep . >> "$work/users" || true
}
status() { # USER ARGS...: the HTTP status only
  u=$1
  shift
  req "$u" -o /dev/null -w '%{http_code}' "$@"
}
create() { # USER: POST /api/v1/sessions; prints the body
  req "$1" -H "$origin" -X POST "$base/api/v1/sessions"
}
created() { # USER: create a session that must succeed; prints its id
  body=$(create "$1")
  echo "$body" | grep -q '"state":"ready"' || fail "$1 create: $body"
  echo "$body" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p'
}
wait_gateway() {
  i=0
  until [ "$(status probe "$base/healthz")" = 200 ]; do
    i=$((i + 1))
    [ $i -lt 60 ] || fail "gateway did not start"
    sleep 1
  done
}
# probe NETWORK URL: the HTTP status a container on NETWORK gets, or "unreachable".
probe() {
  docker run --rm --network "$1" pg-fake-session:it python3 -c '
import sys, urllib.request, urllib.error
try:
    print(urllib.request.urlopen(sys.argv[1], timeout=3).status)
except urllib.error.HTTPError as e:
    print(e.code)
except Exception:
    print("unreachable")' "$2"
}
json() { # EXPR: a Python expression over the JSON document on stdin (j); "" on error
  python3 -c '
import json, sys
try:
    j = json.load(sys.stdin)
    print(eval(sys.argv[1]))
except Exception:
    print("")' "$1"
}

# ---- the stack ---------------------------------------------------------------
compose up -d --quiet-pull >/dev/null 2>&1 || { compose up -d; fail "compose up"; }
wait_gateway

cfg=$(req anon "$base/api/v1/auth/config")
[ "$(echo "$cfg" | json 'j["authMode"], j["oidcDisplayName"]')" = "('oidc', 'Dex')" ] || fail "auth config: $cfg"
[ "$(status anon "$base/api/v1/me")" = 401 ] || fail "anonymous /me"

# Sign-in, refusals and /me.
[ "$(login alice)" = "$base/learn" ] || fail "alice login"
me=$(req alice "$base/api/v1/me")
[ "$(echo "$me" | json 'j["user"]["kind"], j["user"]["email"], j["user"]["groups"]')" = "('oidc', 'alice@example.com', ['it-a'])" ] || fail "alice /me: $me"
[ "$(login mallory)" = "$base/login?error=forbidden" ] || fail "mallory (domain example.org) was not refused"
[ "$(login nora)" = "$base/login?error=forbidden" ] || fail "nora (group it-other) was not refused"
[ "$(status nora "$base/api/v1/me")" = 401 ] || fail "nora has a session after the refusal"
got=$(req probe -o /dev/null -w '%{redirect_url}' "$base/api/v1/auth/oidc/callback?code=x&state=y")
[ "$got" = "$base/login?error=state" ] || fail "callback without the flow cookie: $got"

# The group quota: it-a allows one active session, the default pool one.
id=$(created alice)
[ "$(login amy)" = "$base/learn" ] || fail "amy login"
body=$(create amy)
[ "$(echo "$body" | json 'j["code"], j["details"]["group"], j["details"]["maxActiveSessions"]')" = "('group_quota_exceeded', 'it-a', 1)" ] || fail "amy over the it-a quota: $body"
[ "$(status amy -H "$origin" -X POST "$base/api/v1/sessions")" = 429 ] || fail "amy over the quota: not 429"
[ "$(login bob)" = "$base/learn" ] || fail "bob login"
created bob >/dev/null
[ "$(login brian)" = "$base/learn" ] || fail "brian login"
body=$(create brian)
[ "$(echo "$body" | json 'j["code"], j["details"]["group"]')" = "('group_quota_exceeded', '')" ] || fail "brian over the default pool: $body"

# The session: hardening profile, isolation, proxy, heartbeat.
vol=$(docker inspect "pg-session-$id" --format '{{index .Config.Labels "playground.volume"}}')
profile=$(docker inspect "pg-session-$id" --format '{{.Config.User}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}} {{.HostConfig.Memory}} {{.HostConfig.PidsLimit}} {{range .HostConfig.Ulimits}}{{.Name}}{{end}}')
[ "$profile" = "1000:1000 true [ALL] 268435456 256 nofile" ] || fail "profile: $profile"
docker inspect "pg-session-$id" --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -Eq '^PG_PROXY_SECRET=[0-9a-f]{64}$' || fail "no proxy secret"
nets=$(docker inspect "pg-session-$id" --format '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}')
[ "$nets" = "pg-net-$id " ] || fail "session networks: $nets"
netinfo=$(docker network inspect "pg-net-$id" --format '{{.Internal}} {{range .IPAM.Config}}{{.Subnet}}{{end}}')
case "$netinfo" in "true 10.231."*/28) ;; *) fail "session network: $netinfo" ;; esac
req alice "$base/s/$id/status" | grep -q '"version": "fake"' || fail "proxy"
[ "$(status bob "$base/s/$id/status")" != 200 ] || fail "bob reaches alice's session"
# Another learner's container on the shared network cannot reach the session;
# one on the session's own network gets 401 without the proxy secret.
got=$(probe "$net" "http://pg-session-$id:7000/healthz")
[ "$got" = unreachable ] || fail "the session is reachable from the shared network: $got"
got=$(probe "pg-net-$id" "http://pg-session-$id:7000/status")
[ "$got" = 401 ] || fail "status without the proxy secret: $got"
got=$(probe "pg-net-$id" "http://pg-session-$id:7000/healthz")
[ "$got" = 200 ] || fail "healthz without the proxy secret: $got"
sleep 2
docker logs "pg-session-$id" 2>&1 | grep -q 'heartbeat 204' || fail "heartbeat with the session token"

# Stop frees the quota; the volume stays.
code=$(status alice -H "$origin" -X DELETE "$base/api/v1/sessions/$id")
[ "$code" = 204 ] || fail "delete: $code"
[ -z "$(docker ps -aq --filter "name=pg-session-$id")" ] || fail "container left after stop"
! docker network inspect "pg-net-$id" >/dev/null 2>&1 || fail "network left after stop"
docker volume inspect "$vol" >/dev/null 2>&1 || fail "volume $vol removed on stop"
amy=$(created amy)
[ "$(status amy -H "$origin" -X DELETE "$base/api/v1/sessions/$amy")" = 204 ] || fail "amy delete"
create alice | grep -q "\"id\":\"$id\"" || fail "re-create"

# A new gateway container (an upgrade) on the same data: the registry is
# rebuilt, the gateway joins the running session's network again and the
# quota still counts the running sessions.
compose up -d --force-recreate --no-deps gateway >/dev/null 2>&1 || fail "gateway re-create"
wait_gateway
req alice "$base/api/v1/sessions/$id" | grep -q '"state":"idle"' || fail "registry rebuild"
req alice "$base/s/$id/status" | grep -q '"version": "fake"' || fail "proxy after the gateway was replaced"
[ "$(status amy -H "$origin" -X POST "$base/api/v1/sessions")" = 429 ] || fail "quota after the gateway was replaced"

# Logout: the provider's end_session_endpoint when it advertises one. A
# copy of the cookie taken before is revoked too.
cp "$work/alice.jar" "$work/alice-copy.jar"
end=$(req probe "http://dex:5556/dex/.well-known/openid-configuration" | json 'j.get("end_session_endpoint", "")')
out=$(req alice -H "$origin" -X POST -w '\n%{http_code}' "$base/api/v1/auth/logout")
case "$end:$(echo "$out" | tail -n 1)" in
  :204) ;;
  http*:200) echo "$out" | head -n 1 | grep -q "\"redirectUrl\":\"$end?" || fail "logout redirect: $out" ;;
  *) fail "logout: $out (end_session_endpoint ${end:-none})" ;;
esac
[ "$(status alice "$base/api/v1/me")" = 401 ] || fail "/me after logout"
[ "$(status alice-copy "$base/api/v1/me")" = 401 ] || fail "/me with a cookie copied before the logout"
echo "server-mode test passed"
