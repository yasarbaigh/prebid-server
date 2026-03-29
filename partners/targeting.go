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
	if len(dsp.SourceMap) > 0 {
		src := "web"
		if isApp {
			src = "app"
		}
		if !dsp.SourceMap[src] {
			return false
		}
	}

	// 3. Country matching - normalized to uppercase
	country := ""
	if req.Device != nil && req.Device.Geo != nil {
		country = strings.ToUpper(req.Device.Geo.Country)
	}
	if country != "" {
		// Check Blacklist
		if dsp.CountryBlackListMap[country] {
			return false
		}
		// Check Whitelist
		if len(dsp.CountryMap) > 0 {
			if !dsp.CountryMap[country] && !dsp.CountryMap["ANY"] {
				return false
			}
		}
	}

	// 4. Bundle ID matching (App only) - normalized to lowercase
	if isApp && req.App.Bundle != "" {
		bundle := strings.ToLower(req.App.Bundle)
		// Check Blacklist
		if dsp.BundleIDsBlackListMap[bundle] {
			return false
		}
		// Check Whitelist
		if len(dsp.BundleIDsMap) > 0 {
			if !dsp.BundleIDsMap[bundle] {
				return false
			}
		}
	}

	// 5. SSP Filtering
	if sspID != "" {
		// Check Blacklist
		if dsp.SSPsBlackListMap[sspID] {
			return false
		}
		// Check Whitelist
		if len(dsp.SSPsMap) > 0 {
			if !dsp.SSPsMap[sspID] && !dsp.SSPsMap["ANY"] {
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
	if len(dsp.AdFormatsMap) > 0 {
		formatMatch := false
		for _, imp := range req.Imp {
			if imp.Banner != nil && dsp.AdFormatsMap["banner"] {
				formatMatch = true
				break
			}
			if imp.Video != nil && dsp.AdFormatsMap["video"] {
				formatMatch = true
				break
			}
			if imp.Audio != nil && dsp.AdFormatsMap["audio"] {
				formatMatch = true
				break
			}
			if imp.Native != nil && dsp.AdFormatsMap["native"] {
				formatMatch = true
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
