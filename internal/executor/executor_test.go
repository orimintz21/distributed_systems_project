package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"github.com/orimintz21/distributed_systems_project/internal/sm"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func parseCommand(t *testing.T, text string) *auctionv1.Command {
	t.Helper()
	command := &auctionv1.Command{}
	if err := prototext.Unmarshal([]byte(text), command); err != nil {
		t.Fatal(err)
	}
	return command
}

// TestConcurrentBids verifies that every accepted concurrent bid appears in the ordered history.
func TestConcurrentBids(t *testing.T) {
	e := New(sm.NewState())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go e.Run(ctx)
	const firstAmount = 100
	const count = 200
	create := parseCommand(t, `create_auction { title: "Radio" seller_name: "Dana" starting_price: 100 }`)
	result, err := e.Submit(ctx, create)
	if err != nil || result.GetAuction().GetId() != 1 {
		t.Fatalf("create auction = (%v, %v)", result, err)
	}

	type response struct {
		amount uint64
		result *auctionv1.Result
		err    error
	}
	start := make(chan struct{})
	responses := make(chan response, count)
	for offset := range count {
		amount := uint64(firstAmount + offset)
		go func() {
			<-start
			command := &auctionv1.Command{Kind: &auctionv1.Command_PlaceBid{
				PlaceBid: &auctionv1.PlaceBid{AuctionId: 1, BidderName: "Lee", Amount: amount},
			}}
			result, err := e.Submit(ctx, command)
			responses <- response{amount, result, err}
		}()
	}
	close(start)

	accepted := make(map[uint64]*auctionv1.Bid)
	for range count {
		var reply response
		select {
		case reply = <-responses:
		case <-ctx.Done():
			t.Fatalf("waiting for concurrent bids: %v", ctx.Err())
		}
		if reply.err != nil {
			t.Errorf("submit bid %d: %v", reply.amount, reply.err)
			continue
		}
		switch reply.result.GetCode() {
		case auctionv1.Code_CODE_OK:
			bid := reply.result.GetBid()
			if bid.GetAuctionId() != 1 || bid.GetAmount() != reply.amount || bid.GetId() == 0 {
				t.Errorf("submit bid %d returned %v", reply.amount, bid)
			}
			if _, duplicate := accepted[bid.GetId()]; duplicate {
				t.Errorf("duplicate bid ID %d", bid.GetId())
			}
			accepted[bid.GetId()] = bid
		case auctionv1.Code_CODE_FAILED_PRECONDITION:
			if reply.result.GetMessage() != "bid is too low" {
				t.Errorf("bid %d: %v", reply.amount, reply.result)
			}
		default:
			t.Errorf("bid %d: %v", reply.amount, reply.result)
		}
	}

	result, err = e.Submit(ctx, parseCommand(t, `list_bids { auction_id: 1 }`))
	if err != nil || result.GetBidList() == nil {
		t.Fatalf("list bids = (%v, %v)", result, err)
	}
	var highest uint64
	for _, bid := range result.GetBidList().GetBids() {
		if !proto.Equal(bid, accepted[bid.GetId()]) {
			t.Errorf("stored bid %v differs from accepted bid %v", bid, accepted[bid.GetId()])
		}
		delete(accepted, bid.GetId())
		if bid.GetAmount() <= highest {
			t.Errorf("bids out of acceptance order: %d after %d", bid.GetAmount(), highest)
		}
		highest = bid.GetAmount()
	}
	if len(accepted) != 0 || highest != firstAmount+count-1 {
		t.Errorf("missing accepted bids: %v, highest = %d, want %d", accepted, highest, firstAmount+count-1)
	}
}

// TestRunQueueOrderAndShutdown checks FIFO execution, buffered replies, and shutdown signaling.
func TestRunQueueOrderAndShutdown(t *testing.T) {
	e := New(sm.NewState())
	create := newRequest(parseCommand(t, `create_auction { title: "Radio" seller_name: "Dana" starting_price: 100 }`))
	get := newRequest(parseCommand(t, `get_auction { auction_id: 1 }`))
	e.queue <- create
	e.queue <- get

	runCtx, stop := context.WithCancel(t.Context())
	defer stop()
	go e.Run(runCtx)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := e.await(ctx, get)
	if err != nil || result.GetAuction().GetTitle() != "Radio" {
		t.Fatalf("get queued after create = (%v, %v)", result, err)
	}
	if len(create.reply) != 1 {
		t.Error("create result did not remain buffered")
	}

	stop()
	select {
	case <-e.stopped:
	case <-ctx.Done():
		t.Fatal("Run did not signal shutdown")
	}
	if _, err := e.Submit(ctx, get.cmd); !errors.Is(err, ErrStopped) {
		t.Fatalf("submit after shutdown = %v, want ErrStopped", err)
	}
}

// TestSubmitTimeout checks that a deadline ends both the enqueue and reply waits.
func TestSubmitTimeout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		queueCount int
	}{
		{"waiting for queue space", QueueLen},
		{"waiting for a reply", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(sm.NewState())
			for range tc.queueCount {
				e.queue <- request{}
			}
			command := parseCommand(t, `list_auctions {}`)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			if _, err := e.Submit(ctx, command); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("submit without a worker = %v, want context.DeadlineExceeded", err)
			}
		})
	}
}
