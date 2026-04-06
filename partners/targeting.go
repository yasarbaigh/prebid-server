package partners

import (
	"math/rand"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// MatchTargeting checks if a DSPInventory matches a specific BidRequest and calling SSP
func MatchTargeting(req *openrtb2.BidRequest, dsp *DSPInventory, sspID string, computedTMax int64) bool {
	// 1. Tmax threshold check (Fastest)
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

	// 3. SSP Filtering (High Cardinality / Fast Fail) - retain case
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

	// 4. Country matching - CAPITAL characters - Fast Map Lookup
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

	// 5. Device Type matching
	if req.Device != nil && req.Device.DeviceType > 0 {
		deviceType := int(req.Device.DeviceType)
		if len(dsp.DeviceTypesMap) > 0 {
			if !dsp.DeviceTypesMap[deviceType] {
				return false
			}
		}
	}

	// 6. Bid Floor matching (Min and Max)
	// We check if the SSP floor is high enough for the DSP (Min) AND if the DSP can afford it (Max)
	if dsp.MinBidFloor > 0 || dsp.MaxBidFloor > 0 {
		eligible := false
		for _, imp := range req.Imp {
			match := true
			// If request floor is lower than DSP's minimum requirement, skip
			if dsp.MinBidFloor > 0 && imp.BidFloor < float64(dsp.MinBidFloor) {
				match = false
			}
			// If request floor is higher than DSP's maximum allowed floor, skip
			if match && dsp.MaxBidFloor > 0 && imp.BidFloor > float64(dsp.MaxBidFloor) {
				match = false
			}

			if match {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}

	// 7. Bundle ID matching - lower-case characters
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

	// 7.1 Carrier matching - lower-case characters
	if req.Device != nil && req.Device.Carrier != "" {
		carrier := strings.ToLower(req.Device.Carrier)
		// Check Blacklist
		if dsp.CarrierBlackListMap[carrier] {
			return false
		}
		// Check Whitelist
		if len(dsp.CarrierMap) > 0 {
			if !dsp.CarrierMap[carrier] {
				return false
			}
		}
	}

	// 8. Publisher Filtering - retain case
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

	// 9. Ad Formats matching
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

	// 10. IAB Categories - CAPITAL characters
	if len(dsp.IABCategoriesMap) > 0 {
		catMatch := false
		// Match if ANY of the categories in the request are whitelisted
		for _, cat := range req.BCat {
			if dsp.IABCategoriesMap[strings.ToUpper(cat)] {
				catMatch = true
				break
			}
		}
		// Also check site/app categories
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

		if !catMatch && (len(reqCats) > 0 || len(req.BCat) > 0) {
			return false
		}
	}

	return true
}

func ShortlistDSPs(req *openrtb2.BidRequest, candidates []DSPInventory, sspID string, limit int, computedTMax int64) []DSPInventory {
	n := len(candidates)
	if n == 0 {
		return nil
	}

	// 1. Randomized Fairness using a circular starting point
	// This ensures that we don't always pick the same first 5 DSPs,
	// while avoiding the memory allocation overhead of a full Fisher-Yates shuffle.
	start := 0
	if n > 1 {
		// Note: Using global rand is safe here for a simple offset
		start = rand.Intn(n)
	}

	var shortlisted []DSPInventory
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		dsp := candidates[idx]
		if MatchTargeting(req, &dsp, sspID, computedTMax) {
			shortlisted = append(shortlisted, dsp)
			if len(shortlisted) >= limit {
				break
			}
		}
	}
	return shortlisted
}
