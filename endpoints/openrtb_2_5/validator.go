package openrtb_2_5

import (
	"errors"
	"fmt"
	"github.com/prebid/openrtb/v20/openrtb2"
)

// ValidateBidRequest separates validation logic for 2.5 and 2.6
func ValidateBidRequest(bidReq *openrtb2.BidRequest, version string) error {
	switch version {
	case "2.6":
		return validate26(bidReq)
	default:
		return validate25(bidReq)
	}
}

// 2.5 Validation (Legacy/Current Standard)
func validate25(bidReq *openrtb2.BidRequest) error {
	if bidReq.ID == "" {
		return errors.New("missing request id")
	}
	if len(bidReq.Imp) == 0 {
		return errors.New("no impressions in request")
	}
	for i, imp := range bidReq.Imp {
		if imp.ID == "" {
			return fmt.Errorf("missing imp id at index %d", i)
		}
		if imp.Banner == nil && imp.Video == nil && imp.Native == nil && imp.Audio == nil {
			return fmt.Errorf("imp %s must have at least one of [banner, video, native, audio]", imp.ID)
		}
	}
	return nil
}

// 2.6 Validation (Modern/Future Standard)
// Notes: 2.6 adds better support for CTV, floors, and SupplyChain
func validate26(bidReq *openrtb2.BidRequest) error {
	if err := validate25(bidReq); err != nil {
		return err
	}

	// Future 2.6 specific checks like Schain, multi-deal objects, etc.
	// For now, it passes if 2.5 passes.
	return nil
}
