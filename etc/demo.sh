#!/usr/bin/env sh
# Smoke test of the key-value EXAMPLE (replace with calls to your service).
# Start `make run` (and optionally `make run2`) first.
# Usage: ./etc/demo.sh [base-url]     default: http://localhost:8080
B=${1:-http://localhost:8080}
J='Content-Type: application/json'
set -x
# --- reads: GET -> Read ---------------------------------------------------
curl -si "$B/v1/keys/alpha?consistency=linearizable" | grep -iE '^(HTTP|etag)|^\{' # 200 + ETag
curl -si "$B/v1/keys/missing" | head -1                               # 404
curl -s  "$B/v1/keys?key=gamma&key=alpha&key=missing&sort=-key"       ; echo  # missing skipped
curl -s  "$B/v1/keys?prefix=a&keysOnly=true&limit=2"                   ; echo
curl -si "$B/v1/keys?key=alpha&prefix=a" | head -1                    # 400: two selectors
# --- set: POST -> Update(set) ---------------------------------------------
curl -si -X POST "$B/v1/keys/alpha"   -H "$J" -d '{"value":"42"}' | head -1   # 200 replaced
curl -si -X POST "$B/v1/keys/missing" -H "$J" -d '{"value":"42"}' | head -1   # 201 created
curl -si -X POST "$B/v1/keys/alpha"   -H "$J" -H 'If-Match: "7"' -d '{"value":"x"}' | head -1  # 412
curl -s  -X POST "$B/v1/keys" -H "$J" -d '{"entries":[{"key":"a","value":"1"},{"key":"b","value":"2"}]}' ; echo
# --- read-modify-write: PATCH -> Update(append|increment) -----------------
curl -s  -X PATCH "$B/v1/keys/counter" -H "$J" -d '{"increment":1}'    ; echo
curl -s  -X PATCH "$B/v1/keys/log"     -H "$J" -d '{"append":"!"}'     ; echo
curl -si -X PATCH "$B/v1/keys/log"     -H "$J" -d '{}' | head -1       # 400: no operand
curl -s  -X PATCH "$B/v1/keys" -H "$J" -d '{"ops":[{"key":"c","increment":5},{"key":"l","append":"x"}]}' ; echo
# --- delete: DELETE -> Delete ---------------------------------------------
curl -si -X DELETE "$B/v1/keys/alpha" | head -1                       # 204
curl -s  -X DELETE "$B/v1/keys?key=alpha&key=missing"                 ; echo  # {"deleted":1}
# --- docs -----------------------------------------------------------------
curl -s  "$B/openapi.yaml" | head -3
curl -si "$B/docs"   | head -1                                        # Swagger UI (open in a browser)
curl -si "$B/editor" | head -1                                        # Swagger Editor (open in a browser)
