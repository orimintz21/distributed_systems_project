#!/usr/bin/env sh
# Start `make run`, then run ./etc/demo.sh (node n1).
# Start `make run2`, then run ./etc/demo.sh http://localhost:8081 (forwarded to n1).
# Fixtures: 1=open with bids, 2=closed with bids, 3=open without bids,
# 4=closed without bids; seller name is "seller". Writes are not persisted.
set -eu
B=${1:-http://localhost:8080}
J='Content-Type: application/json'
BODY=$(mktemp)
trap 'rm -f "$BODY"' EXIT

# Display and verify each HTTP status and its JSON response body.
expect() {
  want=$1 method=$2 path=$3
  shift 3
  got=$(curl -sS -o "$BODY" -w '%{http_code}' -X "$method" "$B$path" "$@")
  printf '\n%s %s -> %s (expected %s)\n' "$method" "$path" "$got" "$want"
  cat "$BODY"
  printf '\n'
  if [ "$got" != "$want" ]; then
    printf 'Unexpected status code\n' >&2
    exit 1
  fi
}

# CreateAuction: 201, invalid fields/malformed body -> 400.
expect 201 POST /v1/auctions -H "$J" -d '{"title":"Book","description":"Hardcover","sellerName":"seller","startingPrice":1000}'
expect 400 POST /v1/auctions -H "$J" -d '{"title":"Book","description":"Hardcover","sellerName":"seller","startingPrice":0}'
expect 400 POST /v1/auctions -H "$J" -d '{"startingPrice":1000}'
expect 400 POST /v1/auctions -H "$J" -d '{bad json'

# ListAuctions: 200, unsupported filter or consistency -> 400.
expect 200 GET '/v1/auctions?consistency=linearizable'
expect 200 GET '/v1/auctions?status=open'
expect 200 GET '/v1/auctions?status=closed'
expect 400 GET '/v1/auctions?status=unknown'
expect 400 GET '/v1/auctions?consistency=unknown'

# GetAuction: 200, invalid ID or consistency -> 400, missing auction -> 404.
expect 200 GET /v1/auctions/1
expect 200 GET /v1/auctions/3
expect 400 GET /v1/auctions/-1
expect 400 GET /v1/auctions/18446744073709551616
expect 400 GET '/v1/auctions/1?consistency=unknown'
expect 404 GET /v1/auctions/999

# ListBids: 200 (including []), invalid ID/consistency -> 400, missing -> 404.
expect 200 GET /v1/auctions/1/bids
expect 200 GET /v1/auctions/3/bids
expect 400 GET /v1/auctions/not-a-number/bids
expect 400 GET '/v1/auctions/1/bids?consistency=unknown'
expect 404 GET /v1/auctions/999/bids

# PlaceBid: 201, invalid fields/seller bidding -> 400, missing -> 404,
# closed auction or insufficient amount -> 409.
expect 201 POST /v1/auctions/1/bids -H "$J" -d '{"bidderName":"new-bidder","amount":2500}'
expect 201 POST /v1/auctions/3/bids -H "$J" -d '{"bidderName":"new-bidder","amount":1000}'
expect 400 POST /v1/auctions/1/bids -H "$J" -d '{"bidderName":"seller","amount":2500}'
expect 400 POST /v1/auctions/1/bids -H "$J" -d '{"bidderName":"new-bidder","amount":0}'
expect 404 POST /v1/auctions/999/bids -H "$J" -d '{"bidderName":"new-bidder","amount":2500}'
expect 409 POST /v1/auctions/2/bids -H "$J" -d '{"bidderName":"new-bidder","amount":2500}'
expect 409 POST /v1/auctions/1/bids -H "$J" -d '{"bidderName":"new-bidder","amount":2000}'
expect 409 POST /v1/auctions/3/bids -H "$J" -d '{"bidderName":"new-bidder","amount":999}'

# CloseAuction: 200 (including repeated close and no winner), invalid body ->
# 400, seller mismatch -> 403, missing auction -> 404.
expect 200 PATCH /v1/auctions/1 -H "$J" -d '{"sellerName":"seller","status":"closed"}'
expect 200 PATCH /v1/auctions/2 -H "$J" -d '{"sellerName":"seller","status":"closed"}'
expect 200 PATCH /v1/auctions/3 -H "$J" -d '{"sellerName":"seller","status":"closed"}'
expect 400 PATCH /v1/auctions/1 -H "$J" -d '{"sellerName":"seller","status":"open"}'
expect 403 PATCH /v1/auctions/1 -H "$J" -d '{"sellerName":"other","status":"closed"}'
expect 404 PATCH /v1/auctions/999 -H "$J" -d '{"sellerName":"seller","status":"closed"}'
