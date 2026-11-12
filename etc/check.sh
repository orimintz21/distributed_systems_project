#!/usr/bin/env sh
# Generate, build, and test the auction service, then check both local nodes.
# Requires the tools installed by `make setup`, plus protoc and curl.
set -eu
cd "$(dirname "$0")/.."

step() { printf '\n== %s\n' "$*"; }

step "1/4 generators"
command -v protoc >/dev/null || { echo "protoc not found: https://protobuf.dev/installation/"; exit 1; }
go tool oapi-codegen -version
protoc --version

step "2/4 generate, vet, build, test"
make build
go test ./...

step "3/4 start n1 (:18080/:15051) and n2 (:18081 -> n1)"
CHECK_LOG_DIR=$(mktemp -d "${TMPDIR:-/tmp}/auction-check.XXXXXX")
./bin/server -id n1 -http 127.0.0.1:18080 -grpc 127.0.0.1:15051 >"$CHECK_LOG_DIR/n1.log" 2>&1 &
P1=$!
trap 'kill "$P1" ${P2:-} 2>/dev/null || true' EXIT
./bin/server -id n2 -http 127.0.0.1:18081 -grpc 127.0.0.1:15052 -target 127.0.0.1:15051 >"$CHECK_LOG_DIR/n2.log" 2>&1 &
P2=$!
for port in 18080 18081; do
  ready=false
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS -o /dev/null "http://127.0.0.1:$port/openapi.yaml" 2>/dev/null; then
      ready=true
      break
    fi
    sleep 0.3
  done
  if [ "$ready" != true ]; then
    echo "Server on port $port failed to start; logs: $CHECK_LOG_DIR" >&2
    exit 1
  fi
done

step "4/4 auction operations, errors, and forwarding"
for B in http://127.0.0.1:18080 http://127.0.0.1:18081; do
  ./etc/demo.sh "$B"
done
curl -fsS http://127.0.0.1:18081/v1/auctions/1 | grep -q '"nodeId":"n1"'
curl -sSi http://127.0.0.1:18080/v1/auctions/999 | grep -qi '^content-type: application/problem+json'
printf '\nALL CHECKS PASSED (server logs: %s)\n' "$CHECK_LOG_DIR"
