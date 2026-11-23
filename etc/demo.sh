#!/usr/bin/env sh
# Run against a live server with ./etc/demo.sh [base-url].
set -eu
base=${1:-http://localhost:8080}
json='Content-Type: application/json'
command -v jq >/dev/null || { printf 'jq is required for the demo\n' >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/auction-demo.XXXXXX")
body=$work/body.json
headers=$work/headers
trap 'rm -rf "$work"' EXIT

expect() {
  wanted=$1 method=$2 path=$3
  shift 3
  got=$(curl -sS --max-time 10 -D "$headers" -o "$body" -w '%{http_code}' -X "$method" "$base$path" "$@")
  printf '\n%s %s -> %s (expected %s)\n' "$method" "$path" "$got" "$wanted"
  cat "$body"
  if [ "$got" != "$wanted" ]; then
    printf 'Unexpected status code\n' >&2
    exit 1
  fi
  case "$wanted" in
    4??|5??)
      grep -qi '^content-type: application/problem+json' "$headers"
      jq -e --argjson code "$wanted" '.status == $code and (.detail | length > 0)' "$body" >/dev/null
      ;;
  esac
}

assert_json() {
  jq -e "$@" "$body" >/dev/null || {
    printf 'Unexpected response data\n' >&2
    exit 1
  }
}

if [ "${2:-}" = "--unavailable" ]; then
  expect 503 GET /v1/auctions
  exit 0
fi

expect 201 POST /v1/auctions -H "$json" \
  -d '{"title":"Book","description":"Hardcover","sellerName":"seller","startingPrice":1000}'
book=$(jq -e '.auction.id | numbers' "$body")
expect 201 POST /v1/auctions -H "$json" \
  -d '{"title":"Clock","description":"Desk clock","sellerName":"seller","startingPrice":1000}'
clock=$(jq -e '.auction.id | numbers' "$body")

expect 400 POST /v1/auctions -H "$json" -d '{"title":"Invalid","description":"","sellerName":"seller","startingPrice":0}'
expect 400 POST /v1/auctions -H "$json" -d '{"title":"Invalid","description":"","sellerName":"seller","startingPrice":"invalid"}'
expect 400 POST /v1/auctions -H "$json" -d '{bad json'

expect 200 GET '/v1/auctions?status=open&consistency=linearizable'
assert_json --argjson book "$book" --argjson clock "$clock" \
  'any(.auctions[]; .id == $book) and any(.auctions[]; .id == $clock)'
expect 400 GET '/v1/auctions?status=unknown'
expect 400 GET '/v1/auctions?consistency=unknown'

expect 200 GET "/v1/auctions/$book"
assert_json --argjson id "$book" \
  '.auction | .id == $id and .title == "Book" and (has("highestBid") | not)'
expect 400 GET /v1/auctions/-1
expect 400 GET /v1/auctions/18446744073709551616
expect 400 GET "/v1/auctions/$book?consistency=unknown"
expect 404 GET /v1/auctions/0

expect 200 GET "/v1/auctions/$book/bids"
assert_json '.bids == []'
expect 400 GET /v1/auctions/not-a-number/bids
expect 400 GET "/v1/auctions/$book/bids?consistency=unknown"
expect 404 GET /v1/auctions/0/bids

expect 409 POST "/v1/auctions/$clock/bids" -H "$json" -d '{"bidderName":"Lee","amount":999}'
expect 400 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"seller","amount":1000}'
expect 400 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"Lee","amount":0}'
expect 404 POST /v1/auctions/0/bids -H "$json" -d '{"bidderName":"Lee","amount":1000}'

expect 201 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"Lee","amount":1000}'
first_bid=$(jq -e '.bid.id | numbers' "$body")
expect 201 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"Jo","amount":1250}'
second_bid=$(jq -e '.bid.id | numbers' "$body")
expect 409 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"Lee","amount":1250}'

expect 200 GET "/v1/auctions/$book/bids"
assert_json --argjson first "$first_bid" --argjson second "$second_bid" \
  '(.bids | map(.id)) == [$first, $second] and (.bids | map(.amount)) == [1000, 1250]'
expect 200 GET "/v1/auctions/$clock/bids"
assert_json '.bids == []'

expect 400 PUT "/v1/auctions/$book/closed" -H "$json" -d '{"sellerName":"seller","closed":false}'
expect 403 PUT "/v1/auctions/$book/closed" -H "$json" -d '{"sellerName":"other","closed":true}'
expect 404 PUT /v1/auctions/0/closed -H "$json" -d '{"sellerName":"seller","closed":true}'
expect 404 GET "/v1/auctions/$book/closed"
expect 404 GET "/v1/auctions/$book/winner"
expect 200 GET "/v1/auctions/$book"
assert_json '.auction.highestBid.amount == 1250'
expect 201 PUT "/v1/auctions/$book/closed" -H "$json" -d '{"sellerName":"seller","closed":true}'
assert_json '. == true'
expect 200 PUT "/v1/auctions/$book/closed" -H "$json" -d '{"sellerName":"seller","closed":true}'
assert_json '. == true'
expect 200 GET "/v1/auctions/$book/closed?consistency=linearizable"
assert_json '. == true'
expect 200 GET "/v1/auctions/$book/winner?consistency=linearizable"
assert_json --argjson id "$second_bid" '.bid | .id == $id and .bidderName == "Jo" and .amount == 1250'
expect 409 POST "/v1/auctions/$book/bids" -H "$json" -d '{"bidderName":"Lee","amount":1500}'
expect 201 PUT "/v1/auctions/$clock/closed" -H "$json" -d '{"sellerName":"seller","closed":true}'
expect 404 GET "/v1/auctions/$clock/winner"
expect 200 GET '/v1/auctions?status=closed'
assert_json --argjson book "$book" --argjson clock "$clock" \
  'any(.auctions[]; .id == $book) and any(.auctions[]; .id == $clock) and
   all(.auctions[]; (has("status") or has("winnerName") or has("closed")) | not)'

printf '\nSubmitting concurrent auction creates\n'
pids=
count=12
i=1
while [ "$i" -le "$count" ]; do
  (
    data=$(jq -nc --arg title "Concurrent auction $i" \
      '{title: $title, description: "", sellerName: "seller", startingPrice: 1000}')
    code=$(curl -sS --max-time 10 -o "$work/create-$i.json" -w '%{http_code}' \
      -H "$json" -d "$data" "$base/v1/auctions")
    if [ "$code" != 201 ]; then
      printf 'Concurrent create %s returned %s\n' "$i" "$code" >&2
      cat "$work/create-$i.json" >&2
      exit 1
    fi
  ) &
  pids="$pids $!"
  i=$((i + 1))
done

failed=0
for pid in $pids; do
  wait "$pid" || failed=1
done
[ "$failed" -eq 0 ] || exit 1

jq -es --argjson count "$count" \
  'length == $count and (map(.auction.id | numbers) | unique | length) == $count' \
  "$work"/create-*.json >/dev/null
expect 200 GET '/v1/auctions?status=open'
jq -es --slurpfile stored "$body" \
  'all(.[]; .auction as $created | any($stored[0].auctions[]; . == $created))' \
  "$work"/create-*.json >/dev/null
printf 'Verified all %s concurrent creates in the auction list\n' "$count"
