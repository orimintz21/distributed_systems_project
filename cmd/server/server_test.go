package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"github.com/orimintz21/distributed_systems_project/internal/executor"
	"github.com/orimintz21/distributed_systems_project/internal/grpcsvc"
	"github.com/orimintz21/distributed_systems_project/internal/httpapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func exerciseServer(t *testing.T, snapshotPath string, opts ...grpc.ServerOption) (http.Handler, pb.AuctionServiceClient, func()) {
	t.Helper()
	state, err := loadInitialState(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}

	exec := executor.New(state)
	runCtx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		exec.Run(runCtx)
	}()
	stop := func() {
		cancel()
		<-stopped
	}
	t.Cleanup(stop)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(opts...)
	pb.RegisterAuctionServiceServer(server, grpcsvc.New("test", exec))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///auction", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := pb.NewAuctionServiceClient(conn)
	return httpapi.NewHTTPHandler(client, http.NewServeMux()), client, stop
}

func request(t *testing.T, handler http.Handler, method, target, body string, code int, pieces ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != code {
		t.Fatalf("%s %s = %d, want %d: %s", method, target, res.Code, code, res.Body)
	}
	if code >= 400 && res.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("%s %s: got content type %s", method, target, res.Header().Get("Content-Type"))
	}
	for _, piece := range pieces {
		if !strings.Contains(res.Body.String(), piece) {
			t.Fatalf("%s %s: body %s missing %s", method, target, res.Body, piece)
		}
	}
}

// TestExecutorWiring checks state changes through REST, gRPC stream completion, and shutdown errors.
func TestExecutorWiring(t *testing.T) {
	handler, client, stop := exerciseServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	request(t, handler, "GET", "/v1/auctions", "", 200, `"auctions":[]`)
	request(t, handler, "POST", "/v1/auctions",
		`{"title":"Radio","description":"Shortwave","sellerName":"Dana","startingPrice":100}`, 201, `"id":1`)
	request(t, handler, "POST", "/v1/auctions",
		`{"title":"Clock","description":"Desk clock","sellerName":"Lee","startingPrice":50}`, 201, `"id":2`)
	request(t, handler, "GET", "/v1/auctions/1?consistency=linearizable", "", 200, `"title":"Radio"`, `"status":"open"`)
	request(t, handler, "GET", "/v1/auctions/1/bids", "", 200, `"bids":[]`)
	request(t, handler, "POST", "/v1/auctions/1/bids", `{"bidderName":"Lee","amount":100}`, 201, `"id":1`)
	request(t, handler, "GET", "/v1/auctions/1/bids", "", 200, `"amount":100`)
	request(t, handler, "PATCH", "/v1/auctions/1", `{"sellerName":"Dana","status":"closed"}`, 200, `"status":"closed"`, `"winnerName":"Lee"`)
	request(t, handler, "GET", "/v1/auctions/1", "", 200, `"status":"closed"`, `"winnerName":"Lee"`)
	request(t, handler, "POST", "/v1/auctions/1/bids", `{"bidderName":"Jo","amount":110}`, 409, "auction is closed")
	request(t, handler, "GET", "/v1/auctions/99", "", 404, "auction 99 not found")

	stream, err := client.ListAuctions(ctx, &pb.ListAuctionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for id := uint64(1); id <= 2; id++ {
		msg, err := stream.Recv()
		if err != nil || msg.GetAuction().GetId() != id || msg.GetFin() != (id == 2) {
			t.Fatalf("auction %d: got %v, %v", id, msg, err)
		}
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream after last item: %v", err)
	}

	bids, err := client.ListBids(ctx, &pb.ListBidsRequest{AuctionId: 2})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := bids.Recv()
	if err != nil || !empty.GetFin() || empty.GetBid() != nil {
		t.Fatalf("empty bid stream: got %v, %v", empty, err)
	}
	if _, err := bids.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream after empty final item: %v", err)
	}

	if _, err := client.GetAuction(ctx, &pb.GetAuctionRequest{AuctionId: 1, Consistency: pb.Consistency(99)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid consistency: %v", err)
	}
	stop()
	request(t, handler, "GET", "/v1/auctions/1", "", 503, "executor stopped")
}

// TestExecutorWiringFromSnapshot checks restored state, ID allocation, and malformed snapshot handling.
func TestExecutorWiringFromSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auction.txtpb")
	snapshot := `
		applied: 3
		state {
			last_auction_id: 7
			last_bid_id: 2
			auctions {
				key: 7
				value {
					auction {
						id: 7 title: "Saved auction" seller_name: "Dana" starting_price: 80 status: AUCTION_STATUS_OPEN
					}
					bids { id: 2 auction_id: 7 bidder_name: "Jo" amount: 90 }
				}
			}
		}
	`
	if err := os.WriteFile(path, []byte(snapshot), 0600); err != nil {
		t.Fatal(err)
	}
	handler, _, _ := exerciseServer(t, path)
	request(t, handler, "GET", "/v1/auctions/7", "", 200, `"title":"Saved auction"`, `"amount":90`)
	request(t, handler, "POST", "/v1/auctions/7/bids", `{"bidderName":"Lee","amount":100}`, 201, `"id":3`)
	request(t, handler, "POST", "/v1/auctions",
		`{"title":"Fresh auction","description":"","sellerName":"Jo","startingPrice":100}`, 201, `"id":8`)
	if err := os.WriteFile(path, []byte("invalid snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadInitialState(path); err == nil {
		t.Fatal("invalid snapshot was accepted")
	}
}

// TestBackendErrorResponses checks that backend failures and deadlines become HTTP 500 and 504.
func TestBackendErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		grpcCode codes.Code
		httpCode int
	}{
		{"internal failure", codes.Internal, http.StatusInternalServerError},
		{"deadline exceeded", codes.DeadlineExceeded, http.StatusGatewayTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := grpc.UnaryInterceptor(func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
				return nil, status.Error(tc.grpcCode, tc.name)
			})
			handler, _, _ := exerciseServer(t, "", fault)
			request(t, handler, http.MethodGet, "/v1/auctions/1", "", tc.httpCode, tc.name)
		})
	}
}
