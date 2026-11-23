package sm

import (
	"fmt"
	"strings"
	"testing"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

const (
	radio = `id: 1 title: "Radio" seller_name: "Dana" starting_price: 100`
	clock = `id: 2 title: "Clock" seller_name: "Ari" starting_price: 50`
	lamp  = `
		id: 3
		title: "Lamp"
		description: "Desk lamp"
		seller_name: "Lee"
		starting_price: 75
		status: AUCTION_STATUS_OPEN
	`

	openRadio   = radio + ` status: AUCTION_STATUS_OPEN`
	closedRadio = radio + ` status: AUCTION_STATUS_CLOSED`
	openClock   = clock + ` status: AUCTION_STATUS_OPEN`
	closedClock = clock + ` status: AUCTION_STATUS_CLOSED`

	firstBid    = `id: 1 auction_id: 1 bidder_name: "Lee" amount: 100`
	topRadioBid = `id: 2 auction_id: 1 bidder_name: "Jo" amount: 125`
	newRadioBid = `id: 3 auction_id: 1 bidder_name: "Lee" amount: 150`
	newClockBid = `id: 3 auction_id: 2 bidder_name: "Jo" amount: 50`

	createLamp = `create_auction {
		title: "Lamp"
		description: "Desk lamp"
		seller_name: "Lee"
		starting_price: 75
	}`
)

var (
	openRadioEntry   = auctionEntryText(1, openRadio, firstBid, topRadioBid)
	closedRadioEntry = auctionEntryText(1, closedRadio, firstBid, topRadioBid)
	clockEntry       = auctionEntryText(2, openClock)

	// Insert the entries out of ID order so the data does not match the expected list order.
	fixtureText     = stateText(2, 2, clockEntry, openRadioEntry)
	afterCloseRadio = stateText(2, 2, clockEntry, closedRadioEntry)

	openRadioWithHighest  = openRadio + "\n" + messageText("highest_bid", topRadioBid)
	closedRadioWithWinner = closedRadio + "\n" + messageText("highest_bid", topRadioBid) +
		`winner_name: "Jo"`
)

type commandCase struct {
	name    string
	before  string // empty uses fixtureText
	command string
	result  string
	after   string // empty means unchanged
}

func TestInvalidCommand(t *testing.T) {
	runCommandCases(t, []commandCase{
		{
			name:   "reject an empty command",
			result: errorText(auctionv1.Code_CODE_INVALID_ARGUMENT, "empty command"),
		},
	})
}

func runCommandCases(t *testing.T, tests []commandCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.before
			if before == "" {
				before = fixtureText
			}
			after := tc.after
			if after == "" {
				after = before
			}
			state := parseText(t, before, &auctionv1.State{})
			result := Execute(state, parseText(t, tc.command, &auctionv1.Command{}))
			checkEqual(t, "result", result, parseText(t, tc.result, &auctionv1.Result{}))
			checkEqual(t, "state", state, parseText(t, after, &auctionv1.State{}))
		})
	}
}

func stateText(lastAuctionID, lastBidID uint64, entries ...string) string {
	counters := fmt.Sprintf("last_auction_id: %d\nlast_bid_id: %d\n", lastAuctionID, lastBidID)
	return counters + strings.Join(entries, "")
}

func auctionEntryText(id uint64, auction string, bids ...string) string {
	entry := messageText("value", messageText("auction", auction), repeatedText("bids", bids))
	return messageText("auctions", fmt.Sprintf("key: %d", id), entry)
}

func listText(list, field string, items ...string) string {
	return messageText(list, repeatedText(field, items))
}

func repeatedText(field string, items []string) string {
	var text strings.Builder
	for _, item := range items {
		text.WriteString(messageText(field, item))
	}
	return text.String()
}

func messageText(field string, fields ...string) string {
	return field + " {\n" + strings.Join(fields, "\n") + "\n}\n"
}

func errorText(code auctionv1.Code, message string) string {
	return fmt.Sprintf("code: %s\nmessage: %q", code, message)
}

func parseText[M proto.Message](t *testing.T, text string, message M) M {
	t.Helper()
	if err := prototext.Unmarshal([]byte(text), message); err != nil {
		t.Fatalf("invalid prototext: %v\n%s", err, text)
	}
	return message
}

func checkEqual(t *testing.T, label string, got, want proto.Message) {
	t.Helper()
	if !proto.Equal(got, want) {
		t.Errorf("%s:\ngot:\n%swant:\n%s", label, prototext.Format(got), prototext.Format(want))
	}
}
