#!/usr/bin/env sh
# End-to-end check of the generators and the running servers.
#   ./etc/check.sh        (from the project root; needs Go >= 1.26, protoc, curl)
# 1. installs the generators, 2. generates + vets + builds, 3. starts two nodes,
# 4. calls every operation and compares the HTTP status code with the spec.
set -eu
cd "$(dirname "$0")/.."

step() { printf '\n== %s\n' "$*"; }

step "1/4 generators"
command -v protoc >/dev/null || { echo "protoc not found: https://protobuf.dev/installation/"; exit 1; }
make setup >/dev/null
go tool oapi-codegen -version 2>/dev/null | head -1 || true
protoc --version

step "2/4 generate, vet, build"
make build

step "3/4 start n1 (:18080/:15051) and n2 (:18081 -> n1)"
./bin/server -id n1 -http :18080 -grpc :15051 >/tmp/kvs-n1.log 2>&1 & P1=$!
./bin/server -id n2 -http :18081 -grpc :15052 -target localhost:15051 >/tmp/kvs-n2.log 2>&1 & P2=$!
trap 'kill $P1 $P2 2>/dev/null' EXIT
for i in 1 2 3 4 5 6 7 8 9 10; do curl -s -o /dev/null localhost:18080/openapi.yaml && break; sleep 0.3; done

step "4/4 every operation"
FAIL=0
J='Content-Type: application/json'
# expect <code> <method> <path> [curl args...]
expect() {
  want=$1 m=$2 p=$3; shift 3
  got=$(curl -s -o /tmp/kvs-body -w '%{http_code}' -X "$m" "$B$p" "$@")
  if [ "$got" = "$want" ]; then printf '  ok   %s %-4s %s\n' "$got" "$m" "$p"
  else printf '  FAIL %s %-4s %s (want %s): %s\n' "$got" "$m" "$p" "$want" "$(head -c 200 /tmp/kvs-body)"; FAIL=1; fi
}
for B in http://localhost:18080 http://localhost:18081; do
  echo " $B"
  expect 200 GET    "/v1/keys/alpha?consistency=linearizable"
  expect 404 GET    "/v1/keys/missing"
  expect 200 GET    "/v1/keys?key=alpha&key=missing&sort=-key"
  expect 200 GET    "/v1/keys?prefix=a&keysOnly=true&limit=2"
  expect 400 GET    "/v1/keys?key=alpha&prefix=a"
  expect 400 GET    "/v1/keys?limit=notanumber"
  expect 200 POST   "/v1/keys/alpha"   -H "$J" -d '{"value":"42"}'
  expect 201 POST   "/v1/keys/missing" -H "$J" -d '{"value":"42"}'
  expect 412 POST   "/v1/keys/alpha"   -H "$J" -H 'If-Match: "7"' -d '{"value":"x"}'
  expect 200 POST   "/v1/keys/alpha"   -H "$J" -H 'If-Match: "1"' -d '{"value":"x"}'
  expect 400 POST   "/v1/keys/alpha"   -H "$J" -d '{bad json'
  expect 200 POST   "/v1/keys" -H "$J" -d '{"entries":[{"key":"a","value":"1"},{"key":"b","value":"2"}]}'
  expect 200 PATCH  "/v1/keys/counter" -H "$J" -d '{"increment":1}'
  expect 200 PATCH  "/v1/keys/log"     -H "$J" -d '{"append":"!"}'
  expect 400 PATCH  "/v1/keys/log"     -H "$J" -d '{}'
  expect 200 PATCH  "/v1/keys" -H "$J" -d '{"ops":[{"key":"c","increment":5},{"key":"l","append":"x"}]}'
  expect 204 DELETE "/v1/keys/alpha"
  expect 412 DELETE "/v1/keys/alpha" -H 'If-Match: "9"'
  expect 200 DELETE "/v1/keys?key=alpha&key=missing"
done

# Details a status code does not show.
B=http://localhost:18080
curl -si "$B/v1/keys/alpha" | grep -qi '^etag: "1"' && echo "  ok   ETag header" || { echo "  FAIL ETag header"; FAIL=1; }
curl -si "$B/v1/keys/missing" | grep -qi '^content-type: application/problem+json' && echo "  ok   problem+json errors" || { echo "  FAIL problem+json errors"; FAIL=1; }
curl -s "http://localhost:18081/v1/keys/alpha" | grep -q 'from n1' && echo "  ok   n2 forwards to n1" || { echo "  FAIL n2 forwards to n1"; FAIL=1; }

echo
[ $FAIL = 0 ] && echo "ALL CHECKS PASSED" || { echo "SOME CHECKS FAILED (server logs: /tmp/kvs-n1.log, /tmp/kvs-n2.log)"; exit 1; }
