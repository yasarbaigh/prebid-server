package endpoints

import (
	"errors"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// ValidateBidRequest performs pre-check validations on an incoming SSP bid request.
// It returns an error if the request is invalid according to OpenRTB specs or custom policies.
// ValidateBidRequest performs pre-check validations on an incoming SSP bid request.
// It returns an error if the request is invalid according to OpenRTB specs or custom policies.
func ValidateBidRequest(req *openrtb2.BidRequest) error {
	if req == nil {
		return errors.New("request is nil")
	}

	// 0. Auction ID Check
	if req.ID == "" {
		return errors.New("request is missing required auction id (id)")
	}

	// 1. Mandatory Object Checks (App vs Site)
	if req.App != nil && req.Site != nil {
		return errors.New("request must not have both app and site objects")
	}
	if req.App == nil && req.Site == nil {
		return errors.New("request must contain either app or site object")
	}

	// 2. Impression Check
	if len(req.Imp) == 0 {
		return errors.New("request must contain at least one impression (imp)")
	}

	for _, imp := range req.Imp {
		if imp.ID == "" {
			return errors.New("impression is missing required id")
		}
		// Must have at least one media type
		if imp.Banner == nil && imp.Video == nil && imp.Native == nil && imp.Audio == nil {
			return errors.New("impression must contain at least one of [banner, video, native, audio]")
		}

		// Banner specific: Width/Height or Format check
		if imp.Banner != nil {
			if imp.Banner.W == nil && imp.Banner.H == nil && len(imp.Banner.Format) == 0 {
				return errors.New("banner impression must have dimensions (w, h) or format array")
			}
		}

		// Video specific: Mimes check (DSPs need to know if they can serve the creative)
		if imp.Video != nil {
			if len(imp.Video.MIMEs) == 0 {
				return errors.New("video impression must have mimes list")
			}
		}
	}

	// 3. Publisher Check
	if req.App != nil {
		if req.App.Publisher == nil || req.App.Publisher.ID == "" {
			return errors.New("app.publisher.id is required")
		}
		if req.App.Bundle == "" && req.App.Name == "" {
			return errors.New("app must have either bundle or name")
		}
	} else if req.Site != nil {
		if req.Site.Publisher == nil || req.Site.Publisher.ID == "" {
			return errors.New("site.publisher.id is required")
		}
		if req.Site.Page == "" && req.Site.Domain == "" {
			return errors.New("site must have either page or domain")
		}
	}

	// 4. Device & Geo Check (Critical for DSP Targeting)
	if req.Device == nil {
		return errors.New("device object is required")
	}

	if req.Device.UA == "" {
		return errors.New("device.ua (User Agent) is required")
	}

	if req.Device.IP == "" && req.Device.IPv6 == "" {
		return errors.New("device.ip or device.ipv6 is required")
	}

	if req.Device.Geo != nil {
		country := strings.TrimSpace(req.Device.Geo.Country)
		if country != "" && len(country) != 3 {
			// ISO 3166-1 alpha-3 is the standard in OpenRTB, though many use alpha-2.
			// To be safe, we only return error for empty strings or clearly non-standard lengths if strict.
			// Let's just log or perform basic length check.
			return errors.New("device.geo.country should be ISO-3166-1 Alpha-3 (3 characters)")
		}
	}

	// 5. SupplyChain (SChain) Check
	// If SChain is provided by the SSP, validate its structure
	if req.Source != nil && req.Source.SChain != nil {
		sc := req.Source.SChain
		if sc.Ver == "" {
			return errors.New("source.schain missing version (ver)")
		}
		if len(sc.Nodes) == 0 {
			return errors.New("source.schain nodes list cannot be empty")
		}
		for _, node := range sc.Nodes {
			if node.ASI == "" || node.SID == "" {
				return errors.New("schain node missing required asi or sid")
			}
		}
	}

	// 6. User Check (Gender Validation)
	if req.User != nil && req.User.Gender != "" {
		gender := strings.ToUpper(req.User.Gender)
		if gender != "M" && gender != "F" && gender != "O" {
			// return errors.New("user.gender must be one of [M, F, O]")
		}
	}

	return nil
}
