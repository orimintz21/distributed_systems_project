package sm

import (
	"math"
	"testing"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
)

func TestBidCommands(t *testing.T) {
	runCommandCases(t, []commandCase{
		{
			name:    "list bids in acceptance order",
			command: `list_bids { auction_id: 1 }`,
			result:  listText("bid_list", "bids", firstBid, topRadioBid),
		},
		{
			name:    "list bids on an auction without bids",
			command: `list_bids { auction_id: 2 }`,
			result:  `bid_list {}`,
		},
		{
			name:    "list bids on a missing auction",
			command: `list_bids { auction_id: 99 }`,
			result:  errorText(auctionv1.Code_CODE_NOT_FOUND, "auction 99 not found"),
		},
		{
			name:    "accept a first bid at the starting price with a global bid ID",
			command: `place_bid { auction_id: 2 bidder_name: "Jo" amount: 50 }`,
			result:  messageText("bid", newClockBid),
			after:   stateText(2, 3, openRadioEntry, auctionEntryText(2, openClock, newClockBid)),
		},
		{
			name:    "append a bid above the previous highest",
			command: `place_bid { auction_id: 1 bidder_name: "Lee" amount: 150 }`,
			result:  messageText("bid", newRadioBid),
			after:   stateText(2, 3, auctionEntryText(1, openRadio, firstBid, topRadioBid, newRadioBid), clockEntry),
		},
		{
			name:    "reject a zero bid",
			command: `place_bid { auction_id: 1 bidder_name: "Lee" }`,
			result:  errorText(auctionv1.Code_CODE_INVALID_ARGUMENT, "bid amount must be positive"),
		},
		{
			name:    "reject a bid on a missing auction",
			command: `place_bid { auction_id: 99 bidder_name: "Lee" amount: 150 }`,
			result:  errorText(auctionv1.Code_CODE_NOT_FOUND, "auction 99 not found"),
		},
		{
			name:    "reject a bid from the seller",
			command: `place_bid { auction_id: 1 bidder_name: "Dana" amount: 150 }`,
			result:  errorText(auctionv1.Code_CODE_INVALID_ARGUMENT, "seller cannot bid on their own auction"),
		},
		{
			name:    "reject a bid on a closed auction",
			before:  afterCloseRadio,
			command: `place_bid { auction_id: 1 bidder_name: "Lee" amount: 150 }`,
			result:  errorText(auctionv1.Code_CODE_FAILED_PRECONDITION, "auction is closed"),
		},
		{
			name:    "reject a first bid below the starting price",
			command: `place_bid { auction_id: 2 bidder_name: "Jo" amount: 49 }`,
			result:  errorText(auctionv1.Code_CODE_FAILED_PRECONDITION, "bid is too low"),
		},
		{
			name:    "reject a bid that ties the highest bid without changing state",
			command: `place_bid { auction_id: 1 bidder_name: "Lee" amount: 125 }`,
			result:  errorText(auctionv1.Code_CODE_FAILED_PRECONDITION, "bid is too low"),
		},
		{
			name:    "reject an otherwise valid bid when the final ID check fails",
			before:  stateText(2, math.MaxUint64, openRadioEntry, clockEntry),
			command: `place_bid { auction_id: 1 bidder_name: "Lee" amount: 150 }`,
			result:  errorText(auctionv1.Code_CODE_FAILED_PRECONDITION, "bid IDs exhausted"),
		},
	})
}
