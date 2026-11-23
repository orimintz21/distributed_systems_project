# Auction House

An auction service for creating auctions, inspecting accepted bids, placing bids,
and closing auctions. The client API is defined in [api/openapi.yaml](api/openapi.yaml)
and the internal API in [proto/auction/v1/auction.proto](proto/auction/v1/auction.proto).
See [report.md](report.md) for the service rules and API contract.

```text
client --REST/JSON--> HTTP handlers --gRPC--> AuctionService
                     internal/httpapi      internal/grpcsvc
```

The gRPC service submits each command to an executor. One goroutine applies
commands in queue order to the in-memory auction state.

## Build and run

Requirements: Go >= 1.26.0, `protoc`, Make, a POSIX shell, and `curl`.
The demo and check scripts also use `jq`. Docker and `grpcurl` are optional.

```sh
sudo apt update
sudo apt install protobuf-compiler jq
```

From the project directory:

```sh
make setup   # first use: installs code generators; requires internet
make run     # regenerates, vets, builds, and starts n1
```

`make setup` selects the latest generator versions and may update `go.mod`.
Use `make build` to regenerate, vet, and build `bin/server` without starting it.

In another terminal, create an auction and read it back:

```sh
curl -fsS -H 'Content-Type: application/json' \
  -d '{"title":"Radio","description":"Shortwave","sellerName":"Dana","startingPrice":100}' \
  http://localhost:8080/v1/auctions
curl -fsS http://localhost:8080/v1/auctions/1
```

For forwarding, leave n1 running, start `make run2` in another terminal, and
read the auction through n2:

```sh
curl -fsS http://localhost:8081/v1/auctions/1
```

| Node | REST address | gRPC address | gRPC target used by REST handlers |
| --- | --- | --- | --- |
| n1 | `localhost:8080` | `localhost:50051` | Its own gRPC server |
| n2 | `localhost:8081` | `localhost:50052` | n1 at `localhost:50051` |

The `-id` flag labels each process's startup and shutdown logs. To verify
forwarding, stop n1 and check that n2 returns HTTP 503 as shown below. Stop
manually started servers with Ctrl+C.

## State and snapshots

The server starts with empty state by default. To start from a protobuf-text
`Snapshot` defined in `proto/auction/v1/sm.proto`, pass its file to the server:

```sh
./bin/server -snapshot path/to/snapshot.txtpb
```

Each process owns its local state. Writes affect subsequent requests to the
same gRPC server and live in memory for the lifetime of that process.

## Tests and demonstrations

```sh
make test
make check
```

`make test` regenerates the bindings and runs the Go tests under the race
detector. The tests compare direct and queued results, check concurrent bids,
and exercise REST through an in-memory gRPC connection. The REST tests inject
backend failures to verify HTTP 500 and 504 responses.

`make check` starts two local nodes and runs the REST demo against both. It
checks that writes appear in later reads, rejected bids leave the history
intact, and concurrent creates each appear in the auction list. It also tests
forwarding and HTTP 503 after the backend stops.

To run the demo against an already-running server, use
`./etc/demo.sh http://localhost:8080`. The demo creates its own auctions.

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
| `GET` | `/v1/auctions/{auctionId}/closed` | Read the closure resource | `GetAuction` | 200 |
| `PUT` | `/v1/auctions/{auctionId}/closed` | Close an auction | `CloseAuction` | 201 on creation, 200 on repeat |
| `GET` | `/v1/auctions/{auctionId}/winner` | Get the winning bid | `GetAuction` | 200 |

Reads accept `?consistency=sequential|linearizable`, defaulting to `sequential`.
The choice is passed to gRPC. Writes require linearizability in the service
contract; consistency guarantees are enforced from Part III.

IDs and prices use `uint64`. Prices are positive integer cents. Request and
success bodies use `application/json`. `highestBid` is omitted when there are no
accepted bids.

To close an auction, send `{"sellerName":"Dana","closed":true}` to its
`/closed` URL with PUT. The first successful request returns 201 and a repeat
returns 200. Successful GETs and PUTs on that URL return the JSON boolean
`true`. An open auction has no closure resource.

The `/winner` URL returns `{"bid":{...}}` with the highest accepted bid once
the auction is closed. It returns 404 while the auction is open or if it
closed without bids.

The two list RPCs stream one item per message and set `fin: true` on the
message carrying the last item. An empty list sends one message with just
`fin: true`. HTTP handlers collect the items into a JSON array and return
HTTP 503 if the stream ends without `fin: true`.

Errors use `application/problem+json`. The API returns 400 for invalid requests
or seller bidding, 403 for a seller mismatch when closing, 404 for missing
auctions, closures, or winning bids, and 409 for bids on closed auctions or low
bids. It returns 500 for unexpected failures, 503 when the gRPC service is
unavailable, and 504 when a gRPC deadline expires.

## API documentation and gRPC inspection

With n1 running, open:

- Swagger UI: <http://localhost:8080/docs>
- Swagger Editor: <http://localhost:8080/editor>
- OpenAPI document: <http://localhost:8080/openapi.yaml>

In Swagger UI, use **Try it out** on `POST /v1/auctions` and then read the
returned ID with `GET /v1/auctions/{auctionId}`.

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
grpcurl -plaintext -d '{"auctionId":"1"}' localhost:50051 auction.v1.AuctionService/ListBids
```

## Repository layout

```text
api/openapi.yaml                 REST API specification
api/oapi-codegen.yaml            HTTP code-generation settings
api/spec.go, docs.html, editor.html  Embedded specification and Swagger pages
proto/auction/v1/auction.proto   Internal gRPC service and messages
internal/grpcsvc/                RPC handlers and command and result conversions
internal/executor/               Command queue and executor
internal/sm/                     State machine and command tests
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
