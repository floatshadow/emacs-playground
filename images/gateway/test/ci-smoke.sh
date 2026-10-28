#!/bin/sh
# CI smoke test for a gateway image: starts in local mode, serves health,
# the curriculum (all 15 parts) and the web app. Usage: ci-smoke.sh IMAGE_REF
set -eu
cid=$(docker run -d --rm -p 127.0.0.1:8080:8080 \
  -e PG_MODE=local -e PG_AUTH_MODE=none -e PG_LOCAL_SESSIONURL=http://127.0.0.1:7000 "$1")
trap 'docker logs "$cid" | tail -n 40; docker stop "$cid" >/dev/null' EXIT
health() { curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/healthz; }
for _ in $(seq 1 30); do [ "$(health)" = 200 ] && break; sleep 1; done
test "$(health)" = 200
parts=$(curl -s http://127.0.0.1:8080/api/v1/curriculum | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["parts"]))')
echo "curriculum parts: $parts"
test "$parts" = 15
curl -s http://127.0.0.1:8080/ | grep -qi '<html'
