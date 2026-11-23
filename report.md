# Yuval Lande

# Ori Mintz

<!-- The first page is reserved for feedback. -->
<div style="break-after: page; page-break-after: always;"></div>

# Auction House

## Part I · Service and REST resources

Auction House manages auctions for fictional items. Participants supply seller and bidder names, and the service compares those names as strings. The service assigns unsigned 64-bit auction and bid IDs, and represents prices as positive integer cents.

### Resources and operations

**Auction collection.** Create an auction by supplying a title, description, seller name, and starting price. Creation returns the new auction, which starts open with no accepted bids. List the collection in ascending auction ID order, optionally filtering for open or closed auctions. Listing is a batch read and returns an empty collection when nothing matches.

**Auction.** Read an auction by ID to retrieve its ID, title, description, seller name, starting price, and highest accepted bid if one exists. An unknown ID produces a not-found result.

**Bid collection.** List an auction's accepted bids in acceptance order. An existing auction with no bids returns an empty collection. To place a bid, supply a bidder name and amount. The auction must exist and be open, and the bidder name must differ from its seller name. The first bid must be at least the starting price, and each later bid must exceed the highest accepted bid. Success returns the new bid, which records its own ID, auction ID, bidder name, and amount. Rejection leaves the history unchanged.

**Closure.** Create the auction's closure by supplying the matching seller name and the value `true`. Creation permanently ends bidding. Repeating a valid close returns the existing closure. Read the closure to check whether it exists. Its representation is `true`, and it is absent for an open or unknown auction.

**Winning bid.** Read the winning bid after closure. It has the same representation as an accepted bid and is the last entry in the bid history. The winning bid is absent while the auction is open, if it closed without bids, or if the auction is unknown.

### Operation classes and consistency

| Operations | Class | Required consistency |
| --- | --- | --- |
| List auctions or bids, read an auction, closure, or winning bid | Read | Sequential by default, linearizable on request |
| Create an auction | Write | Linearizable |
| Place a bid or close an auction | Read-modify-write | Linearizable |

**Design decisions.** Explicit closure lets clients decide when bidding ends and makes a concurrent bid and close resolve by execution order. Strictly increasing bids prevent ties and make the winning bid directly derivable from accepted history. These rules live in `internal/sm/sm.go`. Closure and winning bid have their own resources, so clients can retrieve either independently of the auction details.

<div style="break-after: page; page-break-after: always;"></div>

## Part I · RPC and HTTP mapping

`api/openapi.yaml` defines the client contract. `proto/auction/v1/auction.proto` defines `auction.v1.AuctionService`, with a dedicated request and response message for each RPC. `ListAuctions` and `ListBids` are server-streaming RPCs. The other four RPCs are unary.

`{auctionId}` is a decimal unsigned 64-bit ID. All reads accept the optional `consistency` query parameter with values `sequential` and `linearizable`. The auction list also accepts `status=open` or `status=closed`. Request and success bodies use JSON.

| HTTP operation | gRPC method | Success and response | Errors |
| --- | --- | --- | --- |
| POST `/v1/auctions` | `CreateAuction` | 201, `auction` | 409 |
| GET `/v1/auctions` | `ListAuctions` | 200, `auctions` array | |
| GET `/v1/auctions/{auctionId}` | `GetAuction` | 200, `auction` | 404 |
| GET `/v1/auctions/{auctionId}/bids` | `ListBids` | 200, `bids` array | 404 |
| POST `/v1/auctions/{auctionId}/bids` | `PlaceBid` | 201, `bid` | 404, 409 |
| GET `/v1/auctions/{auctionId}/closed` | `GetAuction` | 200, `true` | 404 |
| PUT `/v1/auctions/{auctionId}/closed` | `CloseAuction` | 201 new, 200 repeat, `true` | 403, 404 |
| GET `/v1/auctions/{auctionId}/winner` | `GetAuction` | 200, `bid` | 404 |

The auction creation body contains `title`, `description`, `sellerName`, and `startingPrice`. A bid body contains `bidderName` and `amount`. The closure body contains `sellerName` and `closed: true`. The read operations have no request body.

Every operation can also return 400, 500, 503, or 504. Errors use `application/problem+json` with `type`, `title`, `status`, and `detail`. The adapter in `internal/httpapi/handlers.go` maps RPC errors as follows.

| gRPC status | HTTP response | Meaning |
| --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | Invalid input or seller bidding on their own auction |
| `PERMISSION_DENIED` | 403 | Supplied seller name differs from the auction's seller |
| `NOT_FOUND` | 404 | Unknown auction |
| `FAILED_PRECONDITION` | 409 | Closed auction, insufficient bid, or ID exhaustion |
| `UNAVAILABLE` | 503 | Target unavailable or list stream ended without its final marker |
| `DEADLINE_EXCEEDED` | 504 | Deadline elapsed |
| Other failures | 500 | Unexpected server failure |

**Routing and forwarding.** Generated routing decodes HTTP requests and invokes stateless handlers in `internal/httpapi/handlers.go`. The handlers call a configured gRPC target and translate replies for clients. Closure and winner reads reuse `GetAuction`. The adapter examines the returned status and highest bid and returns 404 if the requested resource is absent. `cmd/server/main.go` wires the local target by default. In the two-node demo, the REST endpoint on node n2 calls gRPC on n1. The Part II executor replaces the Part I dummy backend.

**Status.** Absent required strings can decode as empty strings in create and bid bodies. The OpenAPI create response list omits the 409 returned when auction IDs are exhausted.

<div style="break-after: page; page-break-after: always;"></div>

## Part II · State machine and executor

### State and command representation

`proto/auction/v1/sm.proto` defines `State` as a map of auction IDs to auction details, open or closed status, and accepted bids in order. It also stores the last assigned auction ID and the last assigned bid ID. Both counters are global and start at zero. Reads derive the highest bid from the last accepted bid and derive the winner only after closure.

A `Snapshot` contains a copy of the state and the number of commands applied. `TakeSnapshot` and `Restore` in `internal/sm/sm.go` clone the state so that the snapshot and restored state can be used independently. At startup, `cmd/server/main.go` creates empty state or reads a protobuf text snapshot using `-snapshot`. The snapshot helpers preserve the applied count. The Part II server uses the restored state to start the local executor.

| `Command` variant | Inputs | Successful `Result` |
| --- | --- | --- |
| `CreateAuction` | Title, description, seller name, starting price | New auction |
| `ListAuctions` | Optional status filter | Ordered auction list |
| `GetAuction` | Auction ID | Auction with derived fields |
| `ListBids` | Auction ID | Accepted bid list |
| `PlaceBid` | Auction ID, bidder name, amount | Newly accepted bid |
| `CloseAuction` | Auction ID, seller name | Auction and flag for first closure |

### Request flow

1. **Convert.** The gRPC handlers in `internal/grpcsvc/server.go` pass the request to hand-written converters in `internal/grpcsvc/convert.go`. They construct the appropriate `Command` variant in a protobuf `oneof`. Separate RPC and command messages keep transport options such as read consistency and field masks out of replay.
2. **Execute.** `Submit` sends a command through the bounded queue in `internal/executor/executor.go` and waits for a reply. One goroutine owns the state and calls `Execute` in queue order. Replies are buffered, so a caller that stops waiting does not stall the executor. A timeout after enqueueing can leave a command pending execution.
3. **Return.** A `Result` carries a code and either a typed payload or an error message. Converters turn it into a gRPC reply or status. List RPCs can project fields with an optional field mask and stream one item per message. The last message sets `fin: true`. An empty list sends that marker alone. The REST adapter collects the items and treats an ending without `fin` as a failed read.

### Determinism and atomicity

`Execute` depends only on the current state and command. IDs come from state counters. Auction lists sort numerically by ID, bid histories retain acceptance order, and explicit closure avoids reading a clock. Replaying the same ordered commands from equal states therefore yields equal results and states.

Each write checks its conditions before changing state. For example, placing a bid validates the bidder, auction state, amount, and next ID before appending to history and advancing the counter. A failed final ID check leaves both unchanged. Reads return independent copies, so reply conversion and field masking cannot alter stored state.

**Status.** Both read modes use the same local queue. The forwarding demo uses n1's state for both REST endpoints. Cross-node consistency and log replication are deferred to Part III.

<div style="break-after: page; page-break-after: always;"></div>

## Part II · Verification

The state-machine tests in `internal/sm` use protobuf text fixtures, commands, and expectations. Each table case compares the returned result and full state against its expectations, then checks the same command and state through the executor.

| Test case | Expected result |
| --- | --- |
| Create an auction | Open auction receives the next ID. |
| Zero starting price | Invalid argument. |
| Auction ID exhaustion | Failed precondition before insertion. |
| List all auctions | Auctions returned in ascending ID order. |
| Filter for open auctions | Only open auctions returned. |
| Filter for closed auctions | Only closed auctions returned. |
| Filter with no matches | Empty auction list. |
| Explicit unspecified filter | Invalid argument. |
| Read a known auction | Auction includes its highest accepted bid. |
| Read an unknown auction | Not found. |
| Close with bids | Closure is created and the highest bidder wins. |
| Close without bids | Closure is created with no winner. |
| Repeat closure | Existing closure is returned. |
| Close with the wrong seller | Permission denied. |
| Close a missing auction | Not found. |
| List bids | Accepted bids returned in acceptance order. |
| List bids before the first bid | Empty bid list. |
| List bids on a missing auction | Not found. |
| First bid at the starting price | Accepted with the next global bid ID. |
| Bid above the current highest | Appended to history and becomes the highest bid. |
| Zero bid amount | Invalid argument. |
| Bid on a missing auction | Not found. |
| Seller bids on their auction | Invalid argument. |
| Bid after closure | Failed precondition. |
| First bid below the starting price | Failed precondition. |
| Bid ties the highest | Failed precondition with state unchanged. |
| Bid ID exhaustion | Final check fails with history and counter unchanged. |
| Empty command | Invalid argument. |
| Deterministic replay | Same results and states after each command, including rejection. |
| Snapshot round trip | Restore preserves state, counters, and count without aliasing. |
| 200 concurrent bids | Every accepted bid appears once, ordered up to the maximum offer. |
| Queue order and shutdown | FIFO execution, buffered replies, and shutdown signaling. |
| Deadline waiting for queue space | Submission returns the context deadline error. |
| Deadline waiting for a reply | Submission returns the context deadline error. |
| REST and gRPC wiring | Writes appear in reads, streams end with `fin`, and field masks work. |
| Snapshot startup | REST sees restored data and IDs continue from restored counters. |
| Injected internal error | REST returns 500. |
| Injected RPC deadline | REST returns 504. |
| Live demo | REST operations, rejection checks, and 12 concurrent creates. |
| Forwarding and backend outage | Both REST endpoints see n1's state. n2 returns 503 after n1 stops. |

Executor tests are in `internal/executor/executor_test.go`. Server tests are in `cmd/server/server_test.go`. `make test` runs the tests with the race detector. `make check` runs `etc/demo.sh` against the live servers, including forwarding and backend shutdown.
