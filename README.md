# Auction House — Part I: Application API

An auction service for creating auctions, inspecting accepted bids, placing bids,
and closing auctions. The client API is defined in [api/openapi.yaml](api/openapi.yaml)
and the internal API in [proto/auction/v1/auction.proto](proto/auction/v1/auction.proto).
See [report.md](report.md) for the service rules and API contract.

```text
client --REST/JSON--> HTTP handlers --gRPC--> AuctionService
                     internal/httpapi      internal/grpcsvc
```

Part I uses fixed dummy data. Writes return simulated results without changing
the fixtures. The deterministic state machine and replicated log belong to
Parts II and III.

## Build and run

Requirements: Go >= 1.26.0, `protoc`, Make, a POSIX shell, and `curl`.
Docker and `grpcurl` are optional.

```sh
sudo apt update
sudo apt install protobuf-compiler
```

From the project directory:

```sh
make setup   # first use: installs code generators; requires internet
make run     # regenerates, vets, builds, and starts n1
```

`make setup` selects the latest generator versions and may update `go.mod`.
Use `make build` to regenerate, vet, and build `bin/server` without starting it.

In another terminal:

```sh
./etc/demo.sh http://localhost:8080
```

For forwarding, leave n1 running, start `make run2` in another terminal, then run:

```sh
EXPECTED_NODE_ID=n1 ./etc/demo.sh http://localhost:8081
```

| Node | REST address | gRPC address | gRPC target used by REST handlers |
| --- | --- | --- | --- |
| n1 | `localhost:8080` | `localhost:50051` | Its own gRPC server |
| n2 | `localhost:8081` | `localhost:50052` | n1 at `localhost:50051` |

Successful responses through n2 contain `"nodeId":"n1"`, identifying the gRPC
server that answered. Stop manually started servers with Ctrl+C.

## Demonstration scripts

`etc/demo.sh` sends requests to an already-running server, checks expected HTTP
status codes, and prints response bodies. It also checks the error content type
and, when `EXPECTED_NODE_ID` is set, the responding node ID. It does not assert
every field in the JSON bodies.

To automate the setup:

```sh
make check
```

This runs `etc/check.sh`: regenerate, vet, build, start n1 and n2, and run
`demo.sh` against both. It then stops n1 and runs the unavailable-backend demo
through n2. It uses REST ports 18080/18081 and gRPC ports 15051/15052, cleans up
its servers, and prints `ALL CHECKS PASSED` on success. All API checks run through
`demo.sh`.

To demonstrate an unavailable backend manually, stop n1 while leaving n2 running:

```sh
./etc/demo.sh http://localhost:8081 --unavailable
```

## Auction API

| Method | Path | Operation | gRPC method | Success |
| --- | --- | --- | --- | --- |
| `POST` | `/v1/auctions` | Create an auction | `CreateAuction` | 201 |
| `GET` | `/v1/auctions` | List auctions; optional `status=open` or `status=closed` | `ListAuctions` | 200 |
| `GET` | `/v1/auctions/{auctionId}` | Get an auction | `GetAuction` | 200 |
| `GET` | `/v1/auctions/{auctionId}/bids` | List accepted bids | `ListBids` | 200 |
| `POST` | `/v1/auctions/{auctionId}/bids` | Place a bid | `PlaceBid` | 201 |
| `PATCH` | `/v1/auctions/{auctionId}` | Close an auction | `CloseAuction` | 200 |

Reads accept `?consistency=sequential|linearizable`, defaulting to `sequential`.
The choice is passed to gRPC. Writes require linearizability in the service
contract; consistency guarantees are enforced from Part III.

IDs and prices use `uint64`; prices are positive integer cents. Request and
success bodies use `application/json`. Success bodies contain the result and
`nodeId`. `highestBid` is omitted when there are no accepted bids; `winnerName`
is omitted while open or when closed without bids.

The two list RPCs stream one item per message. HTTP handlers collect the complete
stream into a JSON list response. Empty results contain an empty array and still
identify the responding node.

Errors use `application/problem+json`: 400 for invalid requests or seller bidding,
403 for seller mismatch when closing, 404 for unknown auctions, 409 for closed
auctions or low bids, 500 for unexpected failures, 503 for an unavailable gRPC
service, and 504 for a gRPC deadline exceeded.

## Dummy fixtures

All fixture auctions have seller `seller` and starting price 1000 cents.

| Auction ID | Status | Accepted bid amounts, in order | Winner |
| --- | --- | --- | --- |
| 1 | open | 1500, 2000 | None yet |
| 2 | closed | 1500, 2000 | `bidder` |
| 3 | open | None | None |
| 4 | closed | None | None |

Other auction IDs, such as 999, return 404. Creation always returns auction ID 5;
successful bid placement always returns bid ID 6. These are dummy values: a created
auction cannot subsequently be retrieved, a placed bid does not appear in history,
and closing auction 1 does not change later reads of auction 1.

For auction 1, a new bid must exceed 2000 cents. For auction 3, a first bid of
1000 cents is accepted. The seller cannot bid on their own auction. Closing with
`sellerName: "seller"` returns a simulated closed auction; another seller name
returns 403. Supplied names are business-rule inputs, with no authentication.

## API documentation and gRPC inspection

With n1 running, open:

- Swagger UI: <http://localhost:8080/docs>
- Swagger Editor: <http://localhost:8080/editor>
- OpenAPI document: <http://localhost:8080/openapi.yaml>

Each node serves these pages on its REST port. The Swagger pages load scripts
from a CDN and require internet access. The editor does not save to the repository:
export YAML, overwrite `api/openapi.yaml`, then rebuild to regenerate Go code and
embed the updated specification.

Optional Docker tools are available with `make tools-up`: Swagger Editor on 8090,
Swagger UI on 8091, and Scalar API Reference on 8092. For requests from these pages,
start n1 with `make run CORS='*'` and select `http://localhost:8080` as the API server.
Stop the tool containers with `make tools-down`.

With `grpcurl` installed, reflection allows direct inspection of n1:

```sh
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext -d '{}' localhost:50051 auction.v1.AuctionService/ListAuctions
grpcurl -plaintext -d '{"auctionId":"1"}' localhost:50051 auction.v1.AuctionService/GetAuction
grpcurl -plaintext -d '{"auctionId":"3"}' localhost:50051 auction.v1.AuctionService/ListBids
```

## Repository layout

```text
api/openapi.yaml                 REST API specification
api/oapi-codegen.yaml            HTTP code-generation settings
api/spec.go, docs.html, editor.html  Embedded specification and Swagger pages
proto/auction/v1/auction.proto   Internal gRPC service and messages
internal/grpcsvc/server.go       Dummy fixtures and RPC implementations
internal/httpapi/handlers.go     REST-to-gRPC translation and error mapping
cmd/server/main.go              Server wiring, flags, and forwarding target
etc/demo.sh                     API requests and response checks
etc/check.sh                    Build and two-node demonstration setup
docker/api-tools.compose.yaml   Optional API documentation tools
report.md                       Service semantics and API design
gen/rest/, gen/auction/v1/       Generated Go code; regenerate with make gen
bin/server                      Built executable
```

Edit the OpenAPI and protobuf sources, then regenerate; do not edit generated
files by hand. HTTP handlers hold no application state.
