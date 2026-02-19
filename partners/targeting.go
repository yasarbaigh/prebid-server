package partners

import (
	"strconv"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// MatchTargeting checks if a DSPInventory matches a specific BidRequest
func MatchTargeting(req *openrtb2.BidRequest, dsp *DSPInventory) bool {
	// 1. Source check (App vs Web)
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

	// 2. Country matching
	country := ""
	if req.Device != nil && req.Device.Geo != nil {
		country = strings.ToUpper(req.Device.Geo.Country)
	}
	if country != "" {
		// Check Blacklist
		for _, bc := range dsp.CountryBlackList {
			if strings.ToUpper(bc) == country {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.Country) > 0 {
			whiteMatch := false
			for _, wc := range dsp.Country {
				if strings.ToUpper(wc) == country || strings.ToUpper(wc) == "ANY" {
					whiteMatch = true
					break
				}
			}
			if !whiteMatch {
				return false
			}
		}
	}

	// 3. Bundle ID matching (App only)
	if isApp && req.App.Bundle != "" {
		// Check Blacklist
		for _, bb := range dsp.BundleIDsBlackList {
			if bb == req.App.Bundle {
				return false
			}
		}
		// Check Whitelist
		if len(dsp.BundleIDs) > 0 {
			bundleMatch := false
			for _, wb := range dsp.BundleIDs {
				if wb == req.App.Bundle {
					bundleMatch = true
					break
				}
			}
			if !bundleMatch {
				return false
			}
		}
	}

	// 4. Ad Formats matching
	if len(dsp.AdFormats) > 0 {
		formatMatch := false
		for _, imp := range req.Imp {
			for _, df := range dsp.AdFormats {
				dfLower := strings.ToLower(df)
				if imp.Banner != nil && dfLower == "banner" {
					formatMatch = true
					break
				}
				if imp.Video != nil && dfLower == "video" {
					formatMatch = true
					break
				}
				if imp.Audio != nil && dfLower == "audio" {
					formatMatch = true
					break
				}
				if imp.Native != nil && dfLower == "native" {
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

	// 5. IAB Categories
	if len(dsp.IABCategories) > 0 {
		// This is a bit complex as it can be in req.Bcat or imp level.
		// For simplicity, we check if ANY overlap exists if we had categories in request,
		// but usually Bcat is what DSPs want to AVOID.
		// If dsp.IABCategories contains "Any", it matches.
		hasAny := false
		for _, cat := range dsp.IABCategories {
			if strings.ToLower(cat) == "any" {
				hasAny = true
				break
			}
		}
		if !hasAny {
			// If request has categories (not very common in standard Prebid for targeting filter),
			// we would match here. For now, we assume if it's not "Any" and we don't have request cats to match, we pass or fail?
			// Let's assume most DSPs match a wide range.
		}
	}

	// 6. Bid Floor matching
	if dsp.MinBidFloor != "" {
		minFloor, err := strconv.ParseFloat(dsp.MinBidFloor, 64)
		if err == nil && minFloor > 0 {
			for _, imp := range req.Imp {
				if imp.BidFloor > 0 && imp.BidFloor < minFloor {
					// If the impression floor is lower than DSP min floor, this dsp might not bid.
					// But usually we still send it. However, if ALL imps are below floor, we might skip.
				}
			}
		}
	}

	return true
}

func ShortlistDSPs(req *openrtb2.BidRequest, candidates []DSPInventory, limit int) []DSPInventory {
	var shortlisted []DSPInventory
	for _, dsp := range candidates {
		if MatchTargeting(req, &dsp) {
			shortlisted = append(shortlisted, dsp)
			if len(shortlisted) >= limit {
				break
			}
		}
	}
	return shortlisted
}
