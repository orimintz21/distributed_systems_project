// Package sm implements the deterministic auction state machine.
package sm

import (
	"fmt"
	"math"
	"slices"

	"google.golang.org/protobuf/proto"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
)

// NewState returns an empty auction state.
func NewState() *auctionv1.State {
	return &auctionv1.State{Auctions: make(map[uint64]*auctionv1.AuctionState)}
}

// Execute applies one command. Failed commands leave s unchanged.
func Execute(s *auctionv1.State, c *auctionv1.Command) *auctionv1.Result {
	if s == nil {
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, "missing state")
	}
	switch k := c.GetKind().(type) {
	case *auctionv1.Command_CreateAuction:
		return createAuction(s, k.CreateAuction)
	case *auctionv1.Command_ListAuctions:
		return listAuctions(s, k.ListAuctions)
	case *auctionv1.Command_GetAuction:
		return getAuction(s, k.GetAuction.GetAuctionId())
	case *auctionv1.Command_ListBids:
		return listBids(s, k.ListBids.GetAuctionId())
	case *auctionv1.Command_PlaceBid:
		return placeBid(s, k.PlaceBid)
	case *auctionv1.Command_CloseAuction:
		return closeAuction(s, k.CloseAuction)
	default:
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, "empty command")
	}
}

// TakeSnapshot captures a copy of the state after applied commands.
func TakeSnapshot(s *auctionv1.State, applied int64) *auctionv1.Snapshot {
	return &auctionv1.Snapshot{State: proto.CloneOf(s), Applied: applied}
}

// Restore returns an independent state and its applied command count.
func Restore(snap *auctionv1.Snapshot) (*auctionv1.State, int64) {
	if snap.GetState() == nil {
		return NewState(), snap.GetApplied()
	}
	return proto.CloneOf(snap.GetState()), snap.GetApplied()
}

func createAuction(s *auctionv1.State, c *auctionv1.CreateAuction) *auctionv1.Result {
	if c.GetStartingPrice() == 0 {
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, "starting price must be positive")
	}
	if s.LastAuctionId == math.MaxUint64 {
		return fail(auctionv1.Code_CODE_FAILED_PRECONDITION, "auction IDs exhausted")
	}
	auction := &auctionv1.Auction{
		Id:            s.LastAuctionId + 1,
		Title:         c.GetTitle(),
		Description:   c.GetDescription(),
		SellerName:    c.GetSellerName(),
		StartingPrice: c.GetStartingPrice(),
		Status:        auctionv1.AuctionStatus_AUCTION_STATUS_OPEN,
	}
	if s.Auctions == nil {
		s.Auctions = make(map[uint64]*auctionv1.AuctionState)
	}
	s.Auctions[auction.Id] = &auctionv1.AuctionState{Auction: auction}
	s.LastAuctionId = auction.Id
	return &auctionv1.Result{Content: &auctionv1.Result_Auction{Auction: proto.CloneOf(auction)}}
}

func listAuctions(s *auctionv1.State, c *auctionv1.ListAuctions) *auctionv1.Result {
	filter := c.GetStatus()
	hasFilter := c != nil && c.Status != nil
	if hasFilter && filter != auctionv1.AuctionStatus_AUCTION_STATUS_OPEN && filter != auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED {
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, fmt.Sprintf("unsupported auction status %d", filter))
	}
	ids := make([]uint64, 0, len(s.Auctions))
	for id, entry := range s.Auctions {
		if entry.GetAuction() != nil && (!hasFilter || entry.Auction.GetStatus() == filter) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	auctions := make([]*auctionv1.Auction, 0, len(ids))
	for _, id := range ids {
		auctions = append(auctions, auctionView(s.Auctions[id]))
	}
	return &auctionv1.Result{Content: &auctionv1.Result_AuctionList{
		AuctionList: &auctionv1.AuctionList{Auctions: auctions},
	}}
}

func getAuction(s *auctionv1.State, id uint64) *auctionv1.Result {
	entry := s.Auctions[id]
	if entry.GetAuction() == nil {
		return auctionNotFound(id)
	}
	return &auctionv1.Result{Content: &auctionv1.Result_Auction{Auction: auctionView(entry)}}
}

func listBids(s *auctionv1.State, id uint64) *auctionv1.Result {
	entry := s.Auctions[id]
	if entry.GetAuction() == nil {
		return auctionNotFound(id)
	}
	bids := make([]*auctionv1.Bid, len(entry.Bids))
	for i, bid := range entry.Bids {
		bids[i] = proto.CloneOf(bid)
	}
	return &auctionv1.Result{Content: &auctionv1.Result_BidList{
		BidList: &auctionv1.BidList{Bids: bids},
	}}
}

func placeBid(s *auctionv1.State, c *auctionv1.PlaceBid) *auctionv1.Result {
	amount := c.GetAmount()
	if amount == 0 {
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, "bid amount must be positive")
	}
	id := c.GetAuctionId()
	entry := s.Auctions[id]
	auction := entry.GetAuction()
	if auction == nil {
		return auctionNotFound(id)
	}
	if c.GetBidderName() == auction.GetSellerName() {
		return fail(auctionv1.Code_CODE_INVALID_ARGUMENT, "seller cannot bid on their own auction")
	}
	if auction.GetStatus() == auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED {
		return fail(auctionv1.Code_CODE_FAILED_PRECONDITION, "auction is closed")
	}
	highest := highestBid(entry)
	if amount < auction.GetStartingPrice() || amount <= highest.GetAmount() {
		return fail(auctionv1.Code_CODE_FAILED_PRECONDITION, "bid is too low")
	}
	if s.LastBidId == math.MaxUint64 {
		return fail(auctionv1.Code_CODE_FAILED_PRECONDITION, "bid IDs exhausted")
	}
	bid := &auctionv1.Bid{
		Id:         s.LastBidId + 1,
		AuctionId:  id,
		BidderName: c.GetBidderName(),
		Amount:     amount,
	}
	entry.Bids = append(entry.Bids, bid)
	s.LastBidId = bid.Id
	return &auctionv1.Result{Content: &auctionv1.Result_Bid{Bid: proto.CloneOf(bid)}}
}

func closeAuction(s *auctionv1.State, c *auctionv1.CloseAuction) *auctionv1.Result {
	id := c.GetAuctionId()
	entry := s.Auctions[id]
	auction := entry.GetAuction()
	if auction == nil {
		return auctionNotFound(id)
	}
	if c.GetSellerName() != auction.GetSellerName() {
		return fail(auctionv1.Code_CODE_PERMISSION_DENIED, "seller name does not match")
	}
	auction.Status = auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED
	return &auctionv1.Result{Content: &auctionv1.Result_Auction{Auction: auctionView(entry)}}
}

// Accepted bids increase strictly, so the last accepted bid is the highest.
func highestBid(entry *auctionv1.AuctionState) *auctionv1.Bid {
	if len(entry.Bids) == 0 {
		return nil
	}
	return entry.Bids[len(entry.Bids)-1]
}

// auctionView derives the API fields and keeps the result independent of state.
func auctionView(entry *auctionv1.AuctionState) *auctionv1.Auction {
	auction := proto.CloneOf(entry.Auction)
	auction.HighestBid = proto.CloneOf(highestBid(entry))
	auction.WinnerName = nil
	if auction.Status == auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED && auction.HighestBid != nil {
		auction.WinnerName = proto.String(auction.HighestBid.GetBidderName())
	}
	return auction
}

func auctionNotFound(id uint64) *auctionv1.Result {
	return fail(auctionv1.Code_CODE_NOT_FOUND, fmt.Sprintf("auction %d not found", id))
}

func fail(code auctionv1.Code, message string) *auctionv1.Result {
	return &auctionv1.Result{Code: code, Message: message}
}
