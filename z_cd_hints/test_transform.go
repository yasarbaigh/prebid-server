package main

import (
	"fmt"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/endpoints"
	"github.com/prebid/prebid-server/v3/partners"
)

func main() {
	bid := &openrtb2.Bid{
		ID:    "bid123",
		Price: 1.5,
		NURL:  "http://dsp.com/win?p=${AUCTION_PRICE}",
	}

	ssp := partners.SSPInventory{
		TenantID:       1,
		SSPID:          2,
		SSPInventoryID: 3,
		WinURL:         "http://exchange.com/win",
	}

	dsp := partners.DSPInventory{
		DSPID:          5,
		DSPInventoryID: 6,
		Name:           "My DSP",
	}

	endpoints.TransformWinningBid(bid, ssp, dsp, 1.2, 0.5)

	fmt.Printf("Final NURL: %s\n", bid.NURL)
}
