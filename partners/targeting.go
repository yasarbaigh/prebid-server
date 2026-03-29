package partners

import (
	"strconv"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// MatchTargeting checks if a DSPInventory matches a specific BidRequest and calling SSP
func MatchTargeting(req *openrtb2.BidRequest, dsp *DSPInventory, sspID string, computedTMax int64) bool {
	// 1. Tmax threshold check
	if computedTMax < int64(dsp.Tmax) {
		return false
	}

	// 2. Source check (App vs Web)
	isApp := req.App != nil
	sourceMatch := false
	for _, s := range dsp.Source {
		if (isApp && strings.ToLower(s) == "app") || (!isApp && strings.ToLower(s) == "web") {
			sourceMatch = true
			break
		}
	}
	if !sourceMatch && len(dsp.Source) > 0 {
		return false
	}

	// 3. Country matching - normalized to uppercase
	country := ""
	if req.Device != nil && req.Device.Geo != nil {
		country = strings.ToUpper(req.Device.Geo.Country)
	}
	if country != "" {
		// Check Blacklist
		for _, bc := range dsp.CountryBlackList {
			if bc == country {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.Country) > 0 {
			whiteMatch := false
			for _, wc := range dsp.Country {
				if wc == country || wc == "ANY" {
					whiteMatch = true
					break
				}
			}
			if !whiteMatch {
				return false
			}
		}
	}

	// 4. Bundle ID matching (App only) - normalized to lowercase
	if isApp && req.App.Bundle != "" {
		bundle := strings.ToLower(req.App.Bundle)
		// Check Blacklist
		for _, bb := range dsp.BundleIDsBlackList {
			if bb == bundle {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.BundleIDs) > 0 {
			bundleMatch := false
			for _, wb := range dsp.BundleIDs {
				if wb == bundle {
					bundleMatch = true
					break
				}
			}
			if !bundleMatch {
				return false
			}
		}
	}

	// 5. SSP Filtering
	if sspID != "" {
		// Check Blacklist
		for _, sb := range dsp.SSPsBlackList {
			if sb == sspID {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.SSPs) > 0 {
			sspMatch := false
			for _, ws := range dsp.SSPs {
				if ws == sspID || ws == "ANY" {
					sspMatch = true
					break
				}
			}
			if !sspMatch {
				return false
			}
		}
	}

	// 6. Publisher Filtering
	pubID := ""
	if req.App != nil && req.App.Publisher != nil {
		pubID = req.App.Publisher.ID
	} else if req.Site != nil && req.Site.Publisher != nil {
		pubID = req.Site.Publisher.ID
	}

	if pubID != "" {
		pubID = strings.ToLower(pubID)
		// Check Blacklist
		for _, pb := range dsp.PublishersBlackList {
			if pb == pubID {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.Publishers) > 0 {
			pubMatch := false
			for _, wp := range dsp.Publishers {
				if wp == pubID || strings.ToUpper(wp) == "ANY" {
					pubMatch = true
					break
				}
			}
			if !pubMatch {
				return false
			}
		}
	}

	// 7. Ad Formats matching
	if len(dsp.AdFormats) > 0 {
		formatMatch := false
		for _, imp := range req.Imp {
			for _, df := range dsp.AdFormats {
				if imp.Banner != nil && df == "banner" {
					formatMatch = true
					break
				}
				if imp.Video != nil && df == "video" {
					formatMatch = true
					break
				}
				if imp.Audio != nil && df == "audio" {
					formatMatch = true
					break
				}
				if imp.Native != nil && df == "native" {
					formatMatch = true
					break
				}
			}
			if formatMatch {
				break
			}
		}
		if !formatMatch {
			return false
		}
	}

	// 8. IAB Categories - normalized to uppercase
	if len(dsp.IABCategories) > 0 {
		hasAny := false
		for _, cat := range dsp.IABCategories {
			if cat == "ANY" {
				hasAny = true
				break
			}
		}
		if !hasAny {
			// Basic category check would go here if needed
		}
	}

	// 9. Bid Floor matching
	if dsp.MinBidFloor != "" {
		minFloor, err := strconv.ParseFloat(dsp.MinBidFloor, 64)
		if err == nil && minFloor > 0 {
			allBelow := true
			for _, imp := range req.Imp {
				if imp.BidFloor >= minFloor || imp.BidFloor == 0 {
					allBelow = false
					break
				}
			}
			if allBelow && len(req.Imp) > 0 {
				// Optional: Filter out if all impressions are below minimum floor
				// return false
			}
		}
	}

	return true
}

func ShortlistDSPs(req *openrtb2.BidRequest, candidates []DSPInventory, sspID string, limit int, computedTMax int64) []DSPInventory {
	var shortlisted []DSPInventory
	for _, dsp := range candidates {
		if MatchTargeting(req, &dsp, sspID, computedTMax) {
			shortlisted = append(shortlisted, dsp)
			if len(shortlisted) >= limit {
				break
			}
		}
	}
	return shortlisted
}
