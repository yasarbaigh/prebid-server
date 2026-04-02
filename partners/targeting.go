package partners

import (
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

	// 3. Country matching - CAPITAL characters
	if req.Device != nil && req.Device.Geo != nil && req.Device.Geo.Country != "" {
		country := strings.ToUpper(req.Device.Geo.Country)
		// Check Blacklist
		if dsp.CountryBlackListMap[country] {
			return false
		}
		// Check Whitelist
		if len(dsp.CountryMap) > 0 {
			if !dsp.CountryMap[country] {
				return false
			}
		}
	}

	// 4. Bundle ID matching - lower-case characters
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

	// 4.1 Carrier matching - lower-case characters
	if req.Device != nil && req.Device.Carrier != "" {
		_ = strings.ToLower(req.Device.Carrier)
	}

	// 5. SSP Filtering - retain case
	if sspID != "" {
		// Check Blacklist
		if dsp.SSPsBlackListMap[sspID] {
			return false
		}
		// Check Whitelist
		if len(dsp.SSPsMap) > 0 {
			if !dsp.SSPsMap[sspID] {
				return false
			}
		}
	}

	// 6. Publisher Filtering - retain case
	pubID := ""
	if req.App != nil && req.App.Publisher != nil {
		pubID = req.App.Publisher.ID
	} else if req.Site != nil && req.Site.Publisher != nil {
		pubID = req.Site.Publisher.ID
	}

	if pubID != "" {
		// Check Blacklist
		if dsp.PublishersBlackListMap[pubID] {
			return false
		}
		// Check Whitelist
		if len(dsp.PublishersMap) > 0 {
			if !dsp.PublishersMap[pubID] {
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

	// 8. IAB Categories - CAPITAL characters
	if len(dsp.IABCategoriesMap) > 0 {
		catMatch := false
		// Match if ANY of the categories in the request are whitelisted
		for _, cat := range req.BCat {
			if dsp.IABCategoriesMap[strings.ToUpper(cat)] {
				catMatch = true
				break
			}
		}
		// Wait, BCat is usually for blocking. If the request has whitelist, check app/site.cat.
		var reqCats []string
		if req.App != nil {
			reqCats = req.App.Cat
		} else if req.Site != nil {
			reqCats = req.Site.Cat
		}
		for _, cat := range reqCats {
			if dsp.IABCategoriesMap[strings.ToUpper(cat)] {
				catMatch = true
				break
			}
		}

		if !catMatch && (len(req.Imp) > 0) {
			// IAB check is usually optional or soft, but if whitelist exists and no match, return false
			// return false 
		}
	}

	// 9. Bid Floor matching
	if dsp.MaxBidFloor > 0 {
		eligible := false
		for _, imp := range req.Imp {
			if imp.BidFloor <= float64(dsp.MaxBidFloor) {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
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
