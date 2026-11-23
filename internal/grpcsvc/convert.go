package grpcsvc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
)

func createAuctionCommand(req *auctionv1.CreateAuctionRequest) *auctionv1.Command {
	create := &auctionv1.CreateAuction{
		Title:         req.GetTitle(),
		Description:   req.GetDescription(),
		SellerName:    req.GetSellerName(),
		StartingPrice: req.GetStartingPrice(),
	}
	return &auctionv1.Command{Kind: &auctionv1.Command_CreateAuction{CreateAuction: create}}
}

func listAuctionsCommand(req *auctionv1.ListAuctionsRequest) *auctionv1.Command {
	query := &auctionv1.ListAuctions{}
	if req != nil && req.Status != nil {
		filter := req.GetStatus()
		query.Status = &filter
	}
	return &auctionv1.Command{Kind: &auctionv1.Command_ListAuctions{ListAuctions: query}}
}

func getAuctionCommand(req *auctionv1.GetAuctionRequest) *auctionv1.Command {
	query := &auctionv1.GetAuction{AuctionId: req.GetAuctionId()}
	return &auctionv1.Command{Kind: &auctionv1.Command_GetAuction{GetAuction: query}}
}

func listBidsCommand(req *auctionv1.ListBidsRequest) *auctionv1.Command {
	query := &auctionv1.ListBids{AuctionId: req.GetAuctionId()}
	return &auctionv1.Command{Kind: &auctionv1.Command_ListBids{ListBids: query}}
}

func placeBidCommand(req *auctionv1.PlaceBidRequest) *auctionv1.Command {
	bid := &auctionv1.PlaceBid{
		AuctionId:  req.GetAuctionId(),
		BidderName: req.GetBidderName(),
		Amount:     req.GetAmount(),
	}
	return &auctionv1.Command{Kind: &auctionv1.Command_PlaceBid{PlaceBid: bid}}
}

func closeAuctionCommand(req *auctionv1.CloseAuctionRequest) *auctionv1.Command {
	close := &auctionv1.CloseAuction{AuctionId: req.GetAuctionId(), SellerName: req.GetSellerName()}
	return &auctionv1.Command{Kind: &auctionv1.Command_CloseAuction{CloseAuction: close}}
}

func createAuctionResponse(result *auctionv1.Result) (*auctionv1.CreateAuctionResponse, error) {
	if err := resultStatus(result); err != nil {
		return nil, err
	}
	return &auctionv1.CreateAuctionResponse{Auction: result.GetAuction()}, nil
}

func getAuctionResponse(result *auctionv1.Result) (*auctionv1.GetAuctionResponse, error) {
	if err := resultStatus(result); err != nil {
		return nil, err
	}
	return &auctionv1.GetAuctionResponse{Auction: result.GetAuction()}, nil
}

func placeBidResponse(result *auctionv1.Result) (*auctionv1.PlaceBidResponse, error) {
	if err := resultStatus(result); err != nil {
		return nil, err
	}
	return &auctionv1.PlaceBidResponse{Bid: result.GetBid()}, nil
}

func closeAuctionResponse(result *auctionv1.Result) (*auctionv1.CloseAuctionResponse, error) {
	if err := resultStatus(result); err != nil {
		return nil, err
	}
	return &auctionv1.CloseAuctionResponse{Auction: result.GetAuction()}, nil
}

func sendListAuctionsResult(result *auctionv1.Result, stream auctionv1.AuctionService_ListAuctionsServer) error {
	if err := resultStatus(result); err != nil {
		return err
	}
	auctions := result.GetAuctions()
	if len(auctions) == 0 {
		return stream.Send(&auctionv1.ListAuctionsResponse{Fin: true})
	}
	for i, auction := range auctions {
		if err := stream.Send(&auctionv1.ListAuctionsResponse{Auction: auction, Fin: i == len(auctions)-1}); err != nil {
			return err
		}
	}
	return nil
}

func sendListBidsResult(result *auctionv1.Result, stream auctionv1.AuctionService_ListBidsServer) error {
	if err := resultStatus(result); err != nil {
		return err
	}
	bids := result.GetBids()
	if len(bids) == 0 {
		return stream.Send(&auctionv1.ListBidsResponse{Fin: true})
	}
	for i, bid := range bids {
		if err := stream.Send(&auctionv1.ListBidsResponse{Bid: bid, Fin: i == len(bids)-1}); err != nil {
			return err
		}
	}
	return nil
}

func resultStatus(result *auctionv1.Result) error {
	if result == nil {
		return status.Error(codes.Internal, "missing command result")
	}
	var code codes.Code
	switch result.GetCode() {
	case auctionv1.Code_CODE_OK:
		return nil
	case auctionv1.Code_CODE_INVALID_ARGUMENT:
		code = codes.InvalidArgument
	case auctionv1.Code_CODE_PERMISSION_DENIED:
		code = codes.PermissionDenied
	case auctionv1.Code_CODE_NOT_FOUND:
		code = codes.NotFound
	case auctionv1.Code_CODE_FAILED_PRECONDITION:
		code = codes.FailedPrecondition
	default:
		return status.Errorf(codes.Internal, "unknown command result code %v", result.GetCode())
	}
	return status.Error(code, result.GetMessage())
}
