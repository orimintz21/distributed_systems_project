package sm_test

import (
	"math"
	"testing"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
)

// TestAuctionCommands checks auction creation, reads, and closing through Execute and Submit.
func TestAuctionCommands(t *testing.T) {
	runCommandCases(t, []commandCase{
		{
			name:    "create an open auction with the next ID",
			command: createLamp,
			result:  messageText("auction", lamp),
			after:   stateText(3, 2, openRadioEntry, clockEntry, auctionEntryText(3, lamp)),
		},
		{
			name:    "reject zero starting price",
			command: `create_auction { title: "Lamp" seller_name: "Lee" }`,
			result:  errorText(auctionv1.Code_CODE_INVALID_ARGUMENT, "starting price must be positive"),
		},
		{
			name:    "reject exhausted auction IDs",
			before:  stateText(math.MaxUint64, 2, openRadioEntry, clockEntry),
			command: `create_auction { title: "Lamp" seller_name: "Lee" starting_price: 75 }`,
			result:  errorText(auctionv1.Code_CODE_FAILED_PRECONDITION, "auction IDs exhausted"),
		},
		{
			name:    "list auctions in ascending ID order",
			command: `list_auctions {}`,
			result:  listText("auction_list", "auctions", openRadioWithHighest, openClock),
		},
		{
			name:    "list open auctions",
			before:  afterCloseRadio,
			command: `list_auctions { status: AUCTION_STATUS_OPEN }`,
			result:  listText("auction_list", "auctions", openClock),
		},
		{
			name:    "list closed auctions",
			before:  afterCloseRadio,
			command: `list_auctions { status: AUCTION_STATUS_CLOSED }`,
			result:  listText("auction_list", "auctions", closedRadioWithWinner),
		},
		{
			name:    "list auctions with no matches",
			command: `list_auctions { status: AUCTION_STATUS_CLOSED }`,
			result:  `auction_list {}`,
		},
		{
			name:    "reject an explicit unspecified status filter",
			command: `list_auctions { status: AUCTION_STATUS_UNSPECIFIED }`,
			result:  errorText(auctionv1.Code_CODE_INVALID_ARGUMENT, "unsupported auction status 0"),
		},
		{
			name:    "get an open auction with its highest bid",
			command: `get_auction { auction_id: 1 }`,
			result:  messageText("auction", openRadioWithHighest),
		},
		{
			name:    "get a missing auction",
			command: `get_auction { auction_id: 99 }`,
			result:  errorText(auctionv1.Code_CODE_NOT_FOUND, "auction 99 not found"),
		},
		{
			name:    "close an auction with a winner",
			command: `close_auction { auction_id: 1 seller_name: "Dana" }`,
			result:  messageText("close_auction", messageText("auction", closedRadioWithWinner), "created: true"),
			after:   afterCloseRadio,
		},
		{
			name:    "close an auction with no bids",
			command: `close_auction { auction_id: 2 seller_name: "Ari" }`,
			result:  messageText("close_auction", messageText("auction", closedClock), "created: true"),
			after:   stateText(2, 2, openRadioEntry, auctionEntryText(2, closedClock)),
		},
		{
			name:    "close an already closed auction",
			before:  afterCloseRadio,
			command: `close_auction { auction_id: 1 seller_name: "Dana" }`,
			result:  messageText("close_auction", messageText("auction", closedRadioWithWinner), "created: false"),
		},
		{
			name:    "reject closing with the wrong seller",
			command: `close_auction { auction_id: 1 seller_name: "Lee" }`,
			result:  errorText(auctionv1.Code_CODE_PERMISSION_DENIED, "seller name does not match"),
		},
		{
			name:    "close a missing auction",
			command: `close_auction { auction_id: 99 seller_name: "Dana" }`,
			result:  errorText(auctionv1.Code_CODE_NOT_FOUND, "auction 99 not found"),
		},
	})
}
