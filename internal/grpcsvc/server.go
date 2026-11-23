// Package grpcsvc implements the AuctionService gRPC service.
package grpcsvc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"github.com/orimintz21/distributed_systems_project/internal/executor"
)

// Server serves auction commands through a shared executor.
type Server struct {
	auctionv1.UnimplementedAuctionServiceServer
	NodeID string
	exec   *executor.Executor
}

var _ auctionv1.AuctionServiceServer = (*Server)(nil)

func New(nodeID string, exec *executor.Executor) *Server {
	return &Server{NodeID: nodeID, exec: exec}
}

func (s *Server) submit(ctx context.Context, cmd *auctionv1.Command) (*auctionv1.Result, error) {
	result, err := s.exec.Submit(ctx, cmd)
	if errors.Is(err, executor.ErrStopped) {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if err != nil {
		return nil, status.FromContextError(err).Err()
	}
	return result, nil
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
	result, err := s.submit(ctx, createAuctionCommand(req))
	if err != nil {
		return nil, err
	}
	return createAuctionResponse(result)
}

func (s *Server) ListAuctions(req *auctionv1.ListAuctionsRequest, stream auctionv1.AuctionService_ListAuctionsServer) error {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return err
	}
	mask := req.GetFieldMask()
	if err := validateFieldMask(mask, &auctionv1.Auction{}); err != nil {
		return err
	}
	result, err := s.submit(stream.Context(), listAuctionsCommand(req))
	if err != nil {
		return err
	}
	return sendListAuctionsResult(result, stream, mask)
}

func (s *Server) GetAuction(ctx context.Context, req *auctionv1.GetAuctionRequest) (*auctionv1.GetAuctionResponse, error) {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return nil, err
	}
	result, err := s.submit(ctx, getAuctionCommand(req))
	if err != nil {
		return nil, err
	}
	return getAuctionResponse(result)
}

func (s *Server) ListBids(req *auctionv1.ListBidsRequest, stream auctionv1.AuctionService_ListBidsServer) error {
	if err := validateConsistency(req.GetConsistency()); err != nil {
		return err
	}
	mask := req.GetFieldMask()
	if err := validateFieldMask(mask, &auctionv1.Bid{}); err != nil {
		return err
	}
	result, err := s.submit(stream.Context(), listBidsCommand(req))
	if err != nil {
		return err
	}
	return sendListBidsResult(result, stream, mask)
}

func (s *Server) PlaceBid(ctx context.Context, req *auctionv1.PlaceBidRequest) (*auctionv1.PlaceBidResponse, error) {
	result, err := s.submit(ctx, placeBidCommand(req))
	if err != nil {
		return nil, err
	}
	return placeBidResponse(result)
}

func (s *Server) CloseAuction(ctx context.Context, req *auctionv1.CloseAuctionRequest) (*auctionv1.CloseAuctionResponse, error) {
	result, err := s.submit(ctx, closeAuctionCommand(req))
	if err != nil {
		return nil, err
	}
	return closeAuctionResponse(result)
}
