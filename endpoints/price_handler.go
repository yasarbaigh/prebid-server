package endpoints

import (
	"math"

	"github.com/prebid/openrtb/v20/adcom1"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
)

const DefaultExchangeMargin = 20

// ApplyExchangeMargin processes the DSP response and reduces the bid price by the exchange margin.
// It also ensures that the final price meets the SSP's bid floor requirements.
// Returns a bool indicating if any valid bids remain after the processing.
func ApplyExchangeMargin(resp *openrtb2.BidResponse, bidReq *openrtb2.BidRequest, dsp partners.DSPInventory) bool {
	if resp == nil || bidReq == nil {
		return false
	}

	// 1. Get validated multiplier
	marginMultiplier := GetMarginMultiplier(dsp)
	bidAdjustment := dsp.BidAdjustment
	if bidAdjustment <= 0 {
		bidAdjustment = 1.0 // Default to no adjustment if not set correctly
	}

	var finalSeats []openrtb2.SeatBid

	// 2. Iterate through bids and apply reduction
	for _, sb := range resp.SeatBid {
		var finalBids []openrtb2.Bid
		for _, bid := range sb.Bid {
			// Apply Bid Adjustment (Correction for discrepancies)
			bid.Price = bid.Price * bidAdjustment

			// Apply the Margin to the price
			bid.Price = bid.Price * marginMultiplier

			// Round to 6 decimal places
			bid.Price = math.Round(bid.Price*1000000) / 1000000

			// 3. Find corresponding floor for this specific impression
			var floor float64
			for _, imp := range bidReq.Imp {
				if imp.ID == bid.ImpID {
					floor = imp.BidFloor
					break
				}
			}

			// 4. Creative Attribute Filtering
			if isBlocked(bid.Attr, bidReq) {
				continue
			}

			// 4.5 IAB Category Filtering
			if isCategoryBlocked(bid.Cat, bidReq) {
				continue
			}

			// 5. Verification Check: Only keep bids that stay above the floor
			if bid.Price >= floor {
				finalBids = append(finalBids, bid)
			}
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

// GetMarginMultiplier calculates the multiplier (e.g. 0.8 for 20% margin) for a DSP.
func GetMarginMultiplier(dsp partners.DSPInventory) float64 {
	margin := dsp.Margin
	if margin < 1 || margin > 100 {
		margin = DefaultExchangeMargin
	}
	return (100.0 - float64(margin)) / 100.0
}

// isBlocked checks if any creative attributes in the bid are blocked in the request.
func isBlocked(attr []adcom1.CreativeAttribute, req *openrtb2.BidRequest) bool {
	if len(attr) == 0 || req == nil {
		return false
	}

	// Check against global blocked categories (simplified)
	// Usually Bcat is for categories, Battr is for attributes in individual impressions.
	for _, imp := range req.Imp {
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
	}
	return false
}

// isCategoryBlocked checks if any of the bid categories are blocked in the request.
func isCategoryBlocked(bidCats []string, req *openrtb2.BidRequest) bool {
	if len(bidCats) == 0 || req == nil || len(req.BCat) == 0 {
		return false
	}

	for _, bc := range req.BCat {
		for _, cat := range bidCats {
			if bc == cat {
				return true
			}
		}
	}
	return false
}
