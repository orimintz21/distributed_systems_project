# [Yuval Lande]

# [Ori Mintz]

<!-- Leave the rest of the first PDF page empty for feedback. -->
<div style="break-after: page; page-break-after: always;"></div>

# Auction House

## 1. Service semantics

### 1.1 Purpose and objects

The service manages auctions for fictional items. Users create auctions, inspect
them, place bids, and close them. People are represented by supplied names; this
design does not include a user registry, authentication, payments, or delivery.
There are two domain objects: auctions and accepted bids.

| Auction field | Type | Meaning |
| --- | --- | --- |
| `id` | uint64 | Unique auction identifier assigned by the service |
| `title` | string | Short name of the item |
| `description` | string | Description of the item |
| `sellerName` | string | Name of the seller |
| `startingPrice` | uint64 | Minimum first bid, in integer cents |
| `status` | `open` or `closed` | Whether bidding is permitted |
| `highestBid` | Bid (optional) | Highest accepted bid; omitted before any bid |
| `winnerName` | string (optional) | Name of the highest bidder when closed; otherwise omitted |

| Bid field | Type | Meaning |
| --- | --- | --- |
| `id` | uint64 | Identifier assigned by the service |
| `auctionId` | uint64 | Auction receiving the bid |
| `bidderName` | string | Name of the person placing the bid |
| `amount` | uint64 | Offered amount, in integer cents |

`Auction` is the API representation of an auction and includes derived fields.
`highestBid` and `winnerName` do not require separate domain objects. Rejected bids
are not recorded in bid history. Auction IDs, bid IDs, and prices use unsigned 64-bit
integers (`uint64`); prices must also be greater than zero.

### 1.2 Operations and rules

**Create auction.** Accept item details, a seller name, and a starting price. The
service assigns a unique auction ID and returns an open auction with that ID, no
bids, and no winner.

**List auctions.** Return all auctions, optionally filtered by `open` or `closed`.
Results are ordered numerically by auction ID; no matches produce an empty
list rather than an error.

**Get auction.** Return an auction's details, including its highest bid and final
winner, if any. An unknown auction ID produces a not-found error.

**List bids.** Return an auction's accepted bids in acceptance order. An existing
auction without bids produces an empty list; an unknown auction is an error.

**Place bid.** Accept an auction ID, bidder name, and amount. The auction must exist
and be open, and the bidder name must differ from the seller name. The first bid must be at
least the starting price; subsequent bids must strictly exceed the highest
accepted bid. An accepted bid is added to history and becomes the highest bid.
All checks and changes form one read-modify-write operation. A rejected bid
changes nothing.

**Close auction.** Accept an auction ID and the claimed seller name. The supplied
seller name must match the auction's seller name. Atomically mark the auction closed and
finalize the highest accepted bidder as the winner. If there are no bids, the
auction closes without a winner. Repeating a close request with the correct seller
name returns the existing result without changing it. A closed auction cannot be
reopened. Comparing supplied names is a business rule, not authentication.

There are no automatic deadlines: closing is an explicit operation. After a close
takes effect, all later bids are rejected. If a bid and close compete, their order
in the eventual replicated execution determines whether the bid is accepted.

## 2. Operation categories and consistency

| Operation | Category | Required consistency |
| --- | --- | --- |
| Create auction | State-changing write | Linearizable |
| List auctions | Read | Client chooses sequential or linearizable (Default: sequential) |
| Get auction | Read | Client chooses sequential or linearizable (Default: sequential) |
| List bids | Read | Client chooses sequential or linearizable (Default: sequential) |
| Place bid | Read-modify-write | Linearizable |
| Close auction | Read-modify-write | Linearizable |

## 3. REST API

### 3.1 Operations

All request and success bodies use `application/json`. Path parameter
`auctionId` is a required unsigned 64-bit integer, written in decimal in the URL.
Negative, non-integer, or out-of-range auction IDs return 400. Every read has the consistency parameter
described above. HTTP handlers translate requests and responses without holding
application state.

| Method and path | operationId | Parameters | Request body | Success response | Operation-specific errors |
| --- | --- | --- | --- | --- | --- |
| `POST /v1/auctions` | `createAuction` | None | `CreateAuctionBody` | 201: `AuctionReply` | 400: invalid fields |
| `GET /v1/auctions` | `listAuctions` | Optional `status`; `consistency` | None | 200: `AuctionListReply` | 400: unsupported filter or consistency |
| `GET /v1/auctions/{auctionId}` | `getAuction` | `auctionId`; `consistency` | None | 200: `AuctionReply` | 400: invalid auction ID or consistency; 404: auction missing |
| `GET /v1/auctions/{auctionId}/bids` | `listBids` | `auctionId`; `consistency` | None | 200: `BidListReply` | 400: invalid auction ID or consistency; 404: auction missing |
| `POST /v1/auctions/{auctionId}/bids` | `placeBid` | `auctionId` | `PlaceBidBody` | 201: `BidReply` | 400: invalid fields or seller bidding; 404: auction missing; 409: closed auction or bid too low |
| `PATCH /v1/auctions/{auctionId}` | `closeAuction` | `auctionId` | `CloseAuctionBody` | 200: `AuctionReply`, including repeated close | 400: invalid fields; 403: seller name mismatch; 404: auction missing |

`PATCH` only supports the documented transition to `closed`, it does not provide
general editing of auction fields.

### 3.2 Request and response schemas

All listed fields are required except `Auction.highestBid` and
`Auction.winnerName`, which are omitted when they have no value.

#### Request bodies

| Schema | Fields |
| --- | --- |
| `CreateAuctionBody` | `title`, `description`, `sellerName`: strings; `startingPrice`: uint64 |
| `PlaceBidBody` | `bidderName`: string; `amount`: uint64 |
| `CloseAuctionBody` | `sellerName`: string; `status`: the literal `closed` |

#### Response bodies

| Schema | Fields |
| --- | --- |
| `AuctionReply` | `auction` (Auction) |
| `AuctionListReply` | `auctions` (array of Auction) |
| `BidReply` | `bid` (Bid) |
| `BidListReply` | `bids` (array of Bid) |
| `Problem` | `type`, `title`, `detail`: strings; `status`: integer |

### 3.3 Errors and status mapping

All error responses use `application/problem+json` with the `Problem` schema.
Every operation may return 500, 503, or 504; malformed requests return 400.

| gRPC status | HTTP status | Meaning in the service |
| --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | Invalid request fields or seller attempting to bid |
| `PERMISSION_DENIED` | 403 | Supplied seller name does not match when closing |
| `NOT_FOUND` | 404 | Auction does not exist |
| `FAILED_PRECONDITION` | 409 | Auction is closed or bid does not beat the required amount |
| `UNAVAILABLE` | 503 | Target gRPC service unavailable |
| `DEADLINE_EXCEEDED` | 504 | Request deadline exceeded |
| Other errors | 500 | Unexpected server failure |

## 4. REST-to-RPC mapping

The `auction.v1.AuctionService` gRPC service handles creation, reads, bidding,
and closing. Every RPC has its own request message and response message.
Each REST handler calls the corresponding RPC and translates its result to the
response documented in Section 3. Read operations pass the client's consistency
choice. Errors use gRPC status codes, translated according to Section 3.3.

The four non-list RPCs are unary. `ListAuctions` and `ListBids` send one item per
message and set `fin: true` on the message carrying the last item. An empty
result sends one message with just `fin: true`. The HTTP handler collects the
items into the corresponding JSON list response. If the stream reaches EOF
without a final marker, the handler returns HTTP 503. Other receive errors use
the usual gRPC status mapping.

| REST operation | RPC |
| --- | --- |
| `createAuction` | `CreateAuction` |
| `listAuctions` | `ListAuctions` |
| `getAuction` | `GetAuction` |
| `listBids` | `ListBids` |
| `placeBid` | `PlaceBid` |
| `closeAuction` | `CloseAuction` |

## 5. Design choices

- **Explicit closing:** avoids time-dependent auction deadlines and gives the
  eventual state machine an explicit close command to order against bids.
- **Strictly increasing bids:** prevent tied winning bids and give a simple
  state-dependent condition for the required read-modify-write operation.

## 6. Architecture and Part I implementation

Each server process runs an HTTP server, a gRPC server, and a gRPC client. The
generated HTTP code routes requests and decodes parameters and JSON bodies. The
strict HTTP handlers construct protobuf requests, call the gRPC service, and
convert its results into the generated REST response types. Handlers hold only
the gRPC client, with no application maps or counters.

For example, `GET /v1/auctions/1` reaches `Handlers.GetAuction`, which converts
the consistency option and calls `AuctionService.GetAuction`. `Server.GetAuction`
looks up auction 1 in the dummy fixtures. The HTTP handler uses
`auctionFromProto` to return an `AuctionReply` containing the auction and the
answering node's ID. RPC errors are translated by `httpStatusFromGRPC` and
encoded by `writeProblem`.

| Feature | Implementation location |
| --- | --- |
| REST specification and generated routing | `api/openapi.yaml`, `api/oapi-codegen.yaml`; generated code in `gen/rest/rest.gen.go` |
| Internal API and generated gRPC bindings | `proto/auction/v1/auction.proto`; generated code in `gen/auction/v1/` |
| REST-to-RPC calls and response conversion | `internal/httpapi/handlers.go`: the six `Handlers` methods, `auctionFromProto`, and `bidFromProto` |
| Read consistency option and HTTP error mapping | `internal/httpapi/handlers.go`: `toProtoConsistency`, `httpStatusFromGRPC`, and `writeProblem` |
| Dummy data and business rules | `internal/grpcsvc/server.go`: `dummyAuctions`, `dummyBids`, and the six `Server` RPC methods |
| Server registration and forwarding target | `cmd/server/main.go`: `main` |
| Regeneration and build | `Makefile`: `gen-rest`, `gen-proto`, and `build` |
