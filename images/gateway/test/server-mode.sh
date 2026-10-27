#!/bin/sh
# Server-mode smoke test of the gateway image against a real Docker Engine
# (the docker backend is experimental in slice 1). It builds a stand-in
# session image (fake-session/: /healthz, /status, /rpc/*, a heartbeat with
# its token, answers only with its proxy secret), runs the gateway image
# with invite auth, and checks: login, session creation with the hardening
# profile, the session's own internal network (a container on the shared
# network cannot reach it, one on its network gets 401 without the proxy
# secret), the proxy, the /internal heartbeat, stop (container and network
# removed, volume kept), re-creation, and the registry rebuild after the
# gateway container is replaced (it joins the session's network again).
# Everything it creates is removed at the end.
#
#   images/gateway/test/server-mode.sh [gateway image]
#
# Run from the repository root (on this machine the work directory must be
# under /home/zsy because the Docker daemon is remote).
set -eu
image=${1:-ghcr.io/zsy/emacs-playground/gateway:dev}
port=${PG_IT_PORT:-18081}
base=http://127.0.0.1:$port
origin="Origin: http://localhost:$port"
work=$(pwd)/artifacts/gateway-it-$$
net=pg-it-sessions-$$
gw=pg-gw-it-$$
gwdata=pg-gw-it-data-$$
vol=
id=
mkdir -p "$work"
printf 'it-cohort-code-123 it-cohort uses=5\n' > "$work/invites.txt"
chmod 644 "$work/invites.txt"
jar=$work/cookies.txt

cleanup() {
  docker rm -f "$gw" >/dev/null 2>&1 || true
  if [ -n "$id" ]; then
    docker rm -f "pg-session-$id" >/dev/null 2>&1 || true
    docker network rm "pg-net-$id" >/dev/null 2>&1 || true
  fi
  if [ -n "$vol" ]; then docker volume rm "$vol" >/dev/null 2>&1 || true; fi
  docker volume rm "$gwdata" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
fail() {
  echo "FAIL: $*" >&2
  docker logs "$gw" 2>&1 | tail -20 >&2
  exit 1
}

docker build -q -t pg-fake-session:it "$(dirname "$0")/fake-session" >/dev/null
# The shared sessions network of the Compose file; with isolation network
# (the default) sessions do not join it.
docker network create --internal "$net" >/dev/null
# The image runs as nonroot; the test talks to the local socket as root
# (production uses the socket proxy over TCP, deployment.md §2).
start_gateway() {
  docker run -d --name "$gw" --user 0 -p "127.0.0.1:$port:8080" \
    -v /var/run/docker.sock:/var/run/docker.sock -v "$gwdata:/data" \
    -v "$work/invites.txt:/etc/playground/invites.txt:ro" \
    -e PG_MODE=server -e PG_PUBLIC_URL="http://localhost:$port" -e PG_LOG_FORMAT=text \
    -e PG_AUTH_MODE=invite -e PG_AUTH_INVITES_FILE=/etc/playground/invites.txt \
    -e PG_SESSIONS_IMAGE=pg-fake-session:it -e PG_SESSIONS_NETWORK="$net" \
    -e PG_SESSIONS_REMOTE_BOX_ENABLED=false -e PG_DOCKER_HOST=unix:///var/run/docker.sock \
    -e PG_SESSIONS_LIMITS_MEMORY=256m -e PG_SESSIONS_LIMITS_PIDS=256 \
    "$image" >/dev/null
  docker network connect --alias gateway "$net" "$gw"
  i=0
  until curl -fs "$base/healthz" >/dev/null 2>&1; do
    i=$((i + 1))
    [ $i -lt 20 ] || fail "gateway did not start"
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
start_gateway

code=$(curl -s -o /dev/null -w '%{http_code}' "$base/api/v1/me")
[ "$code" = 401 ] || fail "anonymous /me: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -c "$jar" -b "$jar" -H "$origin" -H 'Content-Type: application/json' -d '{"code":"it-cohort-code-123"}' "$base/api/v1/auth/invite")
[ "$code" = 204 ] || fail "login: $code"
sess=$(curl -s -b "$jar" -H "$origin" -X POST "$base/api/v1/sessions")
echo "$sess" | grep -q '"state":"ready"' || fail "create: $sess"
id=$(echo "$sess" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
vol=$(docker inspect "pg-session-$id" --format '{{index .Config.Labels "playground.volume"}}')
profile=$(docker inspect "pg-session-$id" --format '{{.Config.User}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}} {{.HostConfig.Memory}} {{.HostConfig.PidsLimit}} {{range .HostConfig.Ulimits}}{{.Name}}{{end}}')
[ "$profile" = "1000:1000 true [ALL] 268435456 256 nofile" ] || fail "profile: $profile"
docker inspect "pg-session-$id" --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -Eq '^PG_PROXY_SECRET=[0-9a-f]{64}$' || fail "no proxy secret"
nets=$(docker inspect "pg-session-$id" --format '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}')
[ "$nets" = "pg-net-$id " ] || fail "session networks: $nets"
netinfo=$(docker network inspect "pg-net-$id" --format '{{.Internal}} {{range .IPAM.Config}}{{.Subnet}}{{end}}')
case "$netinfo" in "true 10.231."*/28) ;; *) fail "session network: $netinfo" ;; esac
curl -s -b "$jar" "$base/s/$id/status" | grep -q '"version": "fake"' || fail "proxy"
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
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$jar" -H "$origin" -X DELETE "$base/api/v1/sessions/$id")
[ "$code" = 204 ] || fail "delete: $code"
[ -z "$(docker ps -aq --filter "name=pg-session-$id")" ] || fail "container left after stop"
! docker network inspect "pg-net-$id" >/dev/null 2>&1 || fail "network left after stop"
docker volume inspect "$vol" >/dev/null 2>&1 || fail "volume $vol removed on stop"
curl -s -b "$jar" -H "$origin" -X POST "$base/api/v1/sessions" | grep -q "\"id\":\"$id\"" || fail "re-create"
# A new gateway container (an upgrade) on the same data: the registry is
# rebuilt and the gateway joins the running session's network again.
docker rm -f "$gw" >/dev/null
start_gateway
curl -s -b "$jar" "$base/api/v1/sessions/$id" | grep -q '"state":"idle"' || fail "registry rebuild"
curl -s -b "$jar" "$base/s/$id/status" | grep -q '"version": "fake"' || fail "proxy after the gateway was replaced"
echo "server-mode smoke test passed (session $id)"
