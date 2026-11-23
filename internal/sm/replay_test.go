package sm

import (
	"testing"

	auctionv1 "github.com/orimintz21/distributed_systems_project/gen/auction/v1"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestExecuteDeterminism(t *testing.T) {
	first := parseText(t, fixtureText, &auctionv1.State{})
	second := parseText(t, stateText(2, 2, openRadioEntry, clockEntry), &auctionv1.State{})
	checkEqual(t, "initial state", first, second)

	commands := []struct {
		name string
		text string
	}{
		{"create auction", createLamp},
		{"accept bid", `place_bid { auction_id: 1 bidder_name: "Lee" amount: 150 }`},
		{"close auction", `close_auction { auction_id: 1 seller_name: "Dana" }`},
		{"reject bid after close", `place_bid { auction_id: 1 bidder_name: "Jo" amount: 175 }`},
		{"get auction", `get_auction { auction_id: 1 }`},
		{"list auctions", `list_auctions {}`},
		{"list bids", `list_bids { auction_id: 1 }`},
	}
	for _, step := range commands {
		t.Run(step.name, func(t *testing.T) {
			command := parseText(t, step.text, &auctionv1.Command{})
			firstResult := Execute(first, command)
			secondResult := Execute(second, command)
			checkEqual(t, "result", firstResult, secondResult)
			checkEqual(t, "state", first, second)
		})
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	state := parseText(t, fixtureText, &auctionv1.State{})
	snapshot := TakeSnapshot(state, 4)
	data, err := prototext.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	closeRadio := parseText(t, `close_auction { auction_id: 1 seller_name: "Dana" }`, &auctionv1.Command{})
	Execute(state, closeRadio)
	decoded := parseText(t, string(data), &auctionv1.Snapshot{})
	checkEqual(t, "snapshot after changing live state", snapshot, decoded)

	restored, applied := Restore(decoded)
	if applied != 4 {
		t.Errorf("restored applied count = %d, want 4", applied)
	}
	checkEqual(t, "restored state", restored, parseText(t, fixtureText, &auctionv1.State{}))

	create := parseText(t, createLamp, &auctionv1.Command{})
	wantAuction := parseText(t, messageText("auction", lamp), &auctionv1.Result{})
	checkEqual(t, "create after restore", Execute(restored, create), wantAuction)

	bid := parseText(t, `place_bid { auction_id: 2 bidder_name: "Jo" amount: 50 }`, &auctionv1.Command{})
	wantBid := parseText(t, messageText("bid", newClockBid), &auctionv1.Result{})
	checkEqual(t, "bid after restore", Execute(restored, bid), wantBid)

	Execute(restored, closeRadio)
	checkEqual(t, "snapshot after changing restored state", decoded, snapshot)
}
