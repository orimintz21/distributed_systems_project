// Package grpcsvc implements the AuctionService gRPC service.
//
// STEP 4 — In Part I every RPC returns a dummy response to exercise gRPC calls
// between processes. In Part II this type will apply operations to the state
// machine; in Part III it will submit them to the replicated log.
package grpcsvc

import (
	"context"
	"maps"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
)

// Server serves dummy auctions and identifies the node answering each RPC.
type Server struct {
	auctionv1.UnimplementedAuctionServiceServer
	NodeID string
}

var _ auctionv1.AuctionServiceServer = (*Server)(nil)

func New(nodeID string) *Server { return &Server{NodeID: nodeID} }

// Fixed bid histories in acceptance order. These fixtures are read-only;
// dummy write RPCs return simulated results without updating them.
var dummyBids = map[uint64][]*auctionv1.Bid{
	1: {
		{Id: 1, AuctionId: 1, BidderName: "first-bidder", Amount: 1500},
		{Id: 2, AuctionId: 1, BidderName: "bidder", Amount: 2000},
	},
	2: {
		{Id: 3, AuctionId: 2, BidderName: "first-bidder", Amount: 1500},
		{Id: 4, AuctionId: 2, BidderName: "bidder", Amount: 2000},
	},
}

// Fixture IDs: 1=open with bids, 2=closed with bids, 3=open without bids,
// 4=closed without bids. Other IDs return NOT_FOUND.
var dummyAuctions = map[uint64]*auctionv1.Auction{
	1: {
		Id: 1, Title: "Example item", Description: "Dummy auction",
		SellerName: "seller", StartingPrice: 1000,
		Status:     auctionv1.AuctionStatus_AUCTION_STATUS_OPEN,
		HighestBid: dummyBids[1][1],
	},
	2: {
		Id: 2, Title: "Example item", Description: "Dummy auction",
		SellerName: "seller", StartingPrice: 1000,
		Status:     auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED,
		HighestBid: dummyBids[2][1], WinnerName: proto.String("bidder"),
	},
	3: {
		Id: 3, Title: "Example item", Description: "Dummy auction",
		SellerName: "seller", StartingPrice: 1000,
		Status: auctionv1.AuctionStatus_AUCTION_STATUS_OPEN,
	},
	4: {
		Id: 4, Title: "Example item", Description: "Dummy auction",
		SellerName: "seller", StartingPrice: 1000,
		Status: auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED,
	},
}

func validateConsistency(consistency auctionv1.Consistency) error {
	switch consistency {
	case auctionv1.Consistency_CONSISTENCY_UNSPECIFIED,
		auctionv1.Consistency_CONSISTENCY_SEQUENTIAL,
		auctionv1.Consistency_CONSISTENCY_LINEARIZABLE:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "unsupported consistency %d", consistency)
	}
}

func (s *Server) CreateAuction(ctx context.Context, req *auctionv1.CreateAuctionRequest) (*auctionv1.CreateAuctionResponse, error) {
	if req.GetStartingPrice() == 0 {
		return nil, status.Error(codes.InvalidArgument, "starting price must be positive")
	}
	return &auctionv1.CreateAuctionResponse{
		NodeId: s.NodeID,
		Auction: &auctionv1.Auction{
			Id: 5, Title: req.GetTitle(), Description: req.GetDescription(),
			SellerName: req.GetSellerName(), StartingPrice: req.GetStartingPrice(),
			Status: auctionv1.AuctionStatus_AUCTION_STATUS_OPEN,
		},
	}, nil
}

func (s *Server) ListAuctions(req *auctionv1.ListAuctionsRequest, stream auctionv1.AuctionService_ListAuctionsServer) error {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return err
	}
	if req.Status != nil && req.GetStatus() != auctionv1.AuctionStatus_AUCTION_STATUS_OPEN && req.GetStatus() != auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED {
		return status.Errorf(codes.InvalidArgument, "unsupported auction status %d", req.GetStatus())
	}
	sentAuctions := false
	for _, id := range slices.Sorted(maps.Keys(dummyAuctions)) {
		auction := dummyAuctions[id]
		if req.Status != nil && auction.Status != req.GetStatus() {
			continue
		}
		sentAuctions = true
		if err := stream.Send(&auctionv1.ListAuctionsResponse{
			Auctions: []*auctionv1.Auction{auction}, NodeId: s.NodeID,
		}); err != nil {
			return err
		}
	}
	if !sentAuctions {
		// Empty streams still need a batch identifying the responding node.
		return stream.Send(&auctionv1.ListAuctionsResponse{NodeId: s.NodeID})
	}
	return nil
}

func (s *Server) GetAuction(ctx context.Context, req *auctionv1.GetAuctionRequest) (*auctionv1.GetAuctionResponse, error) {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return nil, err
	}
	auction, ok := dummyAuctions[req.GetAuctionId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "auction %d not found", req.GetAuctionId())
	}
	return &auctionv1.GetAuctionResponse{Auction: auction, NodeId: s.NodeID}, nil
}

func (s *Server) ListBids(req *auctionv1.ListBidsRequest, stream auctionv1.AuctionService_ListBidsServer) error {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return err
	}
	if _, ok := dummyAuctions[req.GetAuctionId()]; !ok {
		return status.Errorf(codes.NotFound, "auction %d not found", req.GetAuctionId())
	}
	bids := dummyBids[req.GetAuctionId()]
	if len(bids) == 0 {
		// Preserve the responding node's ID even when there are no bids.
		return stream.Send(&auctionv1.ListBidsResponse{NodeId: s.NodeID})
	}
	for _, bid := range bids {
		if err := stream.Send(&auctionv1.ListBidsResponse{
			Bids: []*auctionv1.Bid{bid}, NodeId: s.NodeID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) PlaceBid(ctx context.Context, req *auctionv1.PlaceBidRequest) (*auctionv1.PlaceBidResponse, error) {
	if req.GetAmount() == 0 {
		return nil, status.Error(codes.InvalidArgument, "bid amount must be positive")
	}
	auction, ok := dummyAuctions[req.GetAuctionId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "auction %d not found", req.GetAuctionId())
	}
	if req.GetBidderName() == auction.SellerName {
		return nil, status.Error(codes.InvalidArgument, "seller cannot bid on their own auction")
	}
	if auction.Status == auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED {
		return nil, status.Error(codes.FailedPrecondition, "auction is closed")
	}
	if req.GetAmount() < auction.StartingPrice || (auction.HighestBid != nil && req.GetAmount() <= auction.HighestBid.Amount) {
		return nil, status.Error(codes.FailedPrecondition, "bid is too low")
	}
	return &auctionv1.PlaceBidResponse{
		Bid: &auctionv1.Bid{
			Id: 6, AuctionId: auction.Id, BidderName: req.GetBidderName(), Amount: req.GetAmount(),
		},
		NodeId: s.NodeID,
	}, nil
}

func (s *Server) CloseAuction(ctx context.Context, req *auctionv1.CloseAuctionRequest) (*auctionv1.CloseAuctionResponse, error) {
	auction, ok := dummyAuctions[req.GetAuctionId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "auction %d not found", req.GetAuctionId())
	}
	if req.GetSellerName() != auction.SellerName {
		return nil, status.Error(codes.PermissionDenied, "seller name does not match")
	}
	// Work on a copy so closing never modifies the shared dummy data.
	auction = proto.Clone(auction).(*auctionv1.Auction)
	auction.Status = auctionv1.AuctionStatus_AUCTION_STATUS_CLOSED
	if auction.HighestBid != nil {
		auction.WinnerName = proto.String(auction.HighestBid.BidderName)
	}
	return &auctionv1.CloseAuctionResponse{Auction: auction, NodeId: s.NodeID}, nil
}
