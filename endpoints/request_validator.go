package endpoints

import (
	"errors"
	"github.com/buger/jsonparser"
	"github.com/prebid/openrtb/v20/openrtb2"
	"strings"
)

// FastValidateBidRequest performs ultra-fast validation on raw JSON bytes using jsonparser.
// It checks for the bare minimum fields required to even consider the request valid,
// avoiding the high CPU cost of full JSON Unmarshaling for invalid traffic.
func FastValidateBidRequest(body []byte) error {
	// 1. Check Auction ID
	if id, _, _, _ := jsonparser.Get(body, "id"); len(id) == 0 {
		return errors.New("missing mandatory field: id")
	}

	// 2. Check Impressions (must be a non-empty array)
	var count int
	jsonparser.ArrayEach(body, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		count++
	}, "imp")
	if count == 0 {
		return errors.New("missing mandatory field: imp")
	}

	// 3. Check Device Object (usually required for geo/fraud/price check)
	if _, dataType, _, _ := jsonparser.Get(body, "device"); dataType != jsonparser.Object {
		return errors.New("missing mandatory object: device")
	}

	// 4. Check Device Identity
	ip, _, _, _ := jsonparser.Get(body, "device", "ip")
	ipv6, _, _, _ := jsonparser.Get(body, "device", "ipv6")
	if len(ip) == 0 && len(ipv6) == 0 {
		return errors.New("missing mandatory field: device.ip or device.ipv6")
	}

	return nil
}

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
