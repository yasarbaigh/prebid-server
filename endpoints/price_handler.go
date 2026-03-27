package endpoints

import (
	"math"

	"github.com/prebid/openrtb/v20/adcom1"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
)

// ApplyExchangeMargin processes the DSP response and reduces the bid price by the exchange margin.
// It also ensures that the final price meets the SSP's bid floor requirements.
// Returns a bool indicating if any valid bids remain after the processing.
func ApplyExchangeMargin(resp *openrtb2.BidResponse, impMap map[string]*openrtb2.Imp, bcat []string, dsp partners.DSPInventory) bool {
	if resp == nil || impMap == nil {
		return false
	}

	marginMultiplier := GetMarginMultiplier(dsp)
	bidAdjustment := dsp.BidAdjustment
	if bidAdjustment <= 0 {
		bidAdjustment = 1.0
	}

	var finalSeats []openrtb2.SeatBid
	for _, sb := range resp.SeatBid {
		var finalBids []openrtb2.Bid
		for _, bid := range sb.Bid {
			bid.Price = bid.Price * bidAdjustment * marginMultiplier
			bid.Price = math.Round(bid.Price*1000000) / 1000000

			// O(1) Floor Lookup
			imp, ok := impMap[bid.ImpID]
			if !ok || bid.Price < imp.BidFloor {
				continue
			}

			if isAttributeBlocked(bid.Attr, imp) || isCategoryBlocked(bid.Cat, bcat) {
				continue
			}

			finalBids = append(finalBids, bid)
		}

		if len(finalBids) > 0 {
			sb.Bid = finalBids
			finalSeats = append(finalSeats, sb)
		}
	}

	resp.SeatBid = finalSeats
	return len(resp.SeatBid) > 0
}

// GetDspBidRequest clones the original BidRequest and calculates a new bid floor per impression
// based on the DSP's margin. This ensures the DSP bids high enough to cover the exchange margin.
// It also injects the SSP-specific SChain node.
func GetDspBidRequest(orig *openrtb2.BidRequest, ssp partners.SSPInventory, dsp partners.DSPInventory, globalASI string) openrtb2.BidRequest {
	req := *orig // shallow clone is enough as we are replacing the Imp slice

	// Get validated multiplier
	marginMultiplier := GetMarginMultiplier(dsp)

	// Create new imp slice with uplifted floors
	newImps := make([]openrtb2.Imp, len(orig.Imp))
	for i, imp := range orig.Imp {
		newImps[i] = imp
		if imp.BidFloor > 0 {
			newImps[i].BidFloor = imp.BidFloor / marginMultiplier
		}
	}
	req.Imp = newImps

	// 2. Inject SChain via dedicated handler
	AppendSChain(&req, ssp, globalASI)

	return req
}

// GetMarginMultiplier calculates the multiplier (e.g. 0.85 for 15% margin) for a DSP.
func GetMarginMultiplier(dsp partners.DSPInventory) float64 {
	margin := float64(dsp.Margin)
	// Additional safety check (already handled in partners.Load, but good for defensive programming)
	if margin < 0.1 || margin > 100.0 {
		margin = partners.DefaultMargin
	}
	return (100.0 - margin) / 100.0
}

// isAttributeBlocked checks if creative attributes in the bid are blocked for a specific impression.
func isAttributeBlocked(attr []adcom1.CreativeAttribute, imp *openrtb2.Imp) bool {
	if len(attr) == 0 || imp == nil {
		return false
	}

	var battr []adcom1.CreativeAttribute
	if imp.Banner != nil {
		battr = imp.Banner.BAttr
	} else if imp.Video != nil {
		battr = imp.Video.BAttr
	} else if imp.Audio != nil {
		battr = imp.Audio.BAttr
	} else if imp.Native != nil {
		battr = imp.Native.BAttr
	}

	for _, a := range attr {
		for _, b := range battr {
			if a == b {
				return true
			}
		}
	}
	return false
}

// isCategoryBlocked checks if the bid categories are present in the global block list (BCat).
func isCategoryBlocked(bidCats []string, bcat []string) bool {
	if len(bidCats) == 0 || len(bcat) == 0 {
		return false
	}

	for _, bc := range bcat {
		for _, cat := range bidCats {
			if bc == cat {
				return true
			}
		}
	}
	return false
}
