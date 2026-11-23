#!/usr/bin/env sh
# Generate and build the auction service, then run demo.sh against both nodes.
# Requires the tools installed by `make setup`, plus protoc, curl, and jq.
set -eu
cd "$(dirname "$0")/.."

step() { printf '\n== %s\n' "$*"; }

step "1/5 generators"
command -v protoc >/dev/null || {
    echo "protoc not found: https://protobuf.dev/installation/"
    exit 1
}
command -v jq >/dev/null || { printf 'jq is required for the checks\n' >&2; exit 1; }
go tool oapi-codegen -version
protoc --version

step "2/5 generate, vet, build"
make build

step "3/5 start n1 (:18080/:15051) and n2 (:18081 -> n1)"
CHECK_LOG_DIR=$(mktemp -d "${TMPDIR:-/tmp}/auction-check.XXXXXX")
./bin/server -id n1 -http 127.0.0.1:18080 -grpc 127.0.0.1:15051 >"$CHECK_LOG_DIR/n1.log" 2>&1 &
P1=$!
trap 'kill "$P1" ${P2:-} 2>/dev/null || true' EXIT
./bin/server -id n2 -http 127.0.0.1:18081 -grpc 127.0.0.1:15052 -target 127.0.0.1:15051 >"$CHECK_LOG_DIR/n2.log" 2>&1 &
P2=$!
for port in 18080 18081; do
    ready=false
    for attempt in 1 2 3 4 5 6 7 8 9 10; do
        if curl -fsS --max-time 1 -o /dev/null "http://127.0.0.1:$port/openapi.yaml" 2>/dev/null; then
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

step "4/5 auction operations, errors, and forwarding"
n1=http://127.0.0.1:18080
n2=http://127.0.0.1:18081
./etc/demo.sh "$n1"
curl -fsS --max-time 5 "$n1/v1/auctions" >"$CHECK_LOG_DIR/direct.json"
curl -fsS --max-time 5 "$n2/v1/auctions" >"$CHECK_LOG_DIR/forwarded.json"
jq -es '.[0] == .[1] and (.[0].auctions | length > 0)' \
    "$CHECK_LOG_DIR/direct.json" "$CHECK_LOG_DIR/forwarded.json" >/dev/null
./etc/demo.sh "$n2"

step "5/5 unavailable backend: stop n1, then call n2 (expected 503)"
kill "$P1"
wait "$P1"
./etc/demo.sh "$n2" --unavailable
printf '\nALL CHECKS PASSED (server logs: %s)\n' "$CHECK_LOG_DIR"
