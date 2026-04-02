package openrtb_2_5

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	jsoniter "github.com/json-iterator/go"
	"github.com/julienschmidt/httprouter"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/endpoints"
	"github.com/prebid/prebid-server/v3/logger"
	"github.com/prebid/prebid-server/v3/logging"
	"github.com/prebid/prebid-server/v3/partners"
	"github.com/prebid/prebid-server/v3/proto/generated"
)

const (
	ExchangeOverhead = 120
)

var (
	json = jsoniter.ConfigCompatibleWithStandardLibrary
)

type bidResult struct {
	resp        *openrtb2.BidResponse
	dsp         partners.DSPInventory
	reqBody     []byte
	dspRespBody []byte
}

type AuctionHandler struct {
	PartnersManager *partners.Manager
	HttpClient      *http.Client
	GlobalASI       string
	Hostname        string
}

func NewAuctionHandler(pm *partners.Manager) *AuctionHandler {
	globalASI := "my-ad-exchange.com" // Final fallback
	if pm != nil {
		cfg := pm.GetConfig()
		if cfg != nil && cfg.ASI != "" {
			globalASI = cfg.ASI
		}
	}

	return &AuctionHandler{
		PartnersManager: pm,
		GlobalASI:       globalASI,
		HttpClient: &http.Client{
			Timeout: 500 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		Hostname: func() string {
			h, _ := os.Hostname()
			return h
		}(),
	}
}

func (h *AuctionHandler) Handle(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Panic Recovery
	defer func() {
		if r := recover(); r != nil {
			errDetail := fmt.Sprintf("PANIC RECOVERED: %v", r)
			logger.Errorf(errDetail)
			w.WriteHeader(http.StatusNoContent)
		}
	}()

	// 1. Health check
	cfg := h.PartnersManager.GetConfig()
	if cfg == nil || !h.PartnersManager.IsHealthy() || !cfg.AdServing {
		partners.AuctionCounter.WithLabelValues("unknown", "unknown", "unknown", "rejected_unhealthy_config").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Identify SSP
	accountCode := r.URL.Query().Get("c")
	if accountCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ssp, ok := h.PartnersManager.GetSSPByInventoryCode(accountCode)
	if !ok {
		h.recordSSPResponse(nil, "error", "400")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 2.1 Metrics: Record SSP Request
	partners.SSPRequestCounter.WithLabelValues(ssp.SSPInventoryIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier).Inc()

	// 3. Read & Parse Body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2*1024*1024))
	if err != nil {
		h.recordSSPResponse(ssp, "error", "400")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 4. PRE-CHECK TMAX (Fast check using jsonparser before unmarshaling)
	bidLogger := logging.GetBidLogger()
	isSampled := bidLogger != nil && bidLogger.ShouldSampleVerbose()
	if isSampled {
		bidLogger.LogSSP(ssp.SSPInventoryIdentifier, body, "REQ")
	}

	originalTMax, err := jsonparser.GetInt(body, "tmax")
	if err != nil {
		originalTMax = 500 // Default if missing
	}
	computedTMax := originalTMax - ExchangeOverhead
	if computedTMax < 120 {
		partners.AuctionCounter.WithLabelValues(ssp.SSPInventoryIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "rejected_tmax").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var bidReq openrtb2.BidRequest
	if err := json.Unmarshal(body, &bidReq); err != nil {
		h.recordSSPResponse(ssp, "error", "400")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 5. Versioning & Validation
	bidReq.TMax = computedTMax
	version := r.Header.Get("X-OpenRTB-Version")
	if version == "" {
		version = "2.5" // Default legacy
	}

	if err := ValidateBidRequest(&bidReq, version); err != nil {
		partners.SSPValidationFailedCounter.WithLabelValues(ssp.SSPInventoryIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier).Inc()
		h.recordSSPResponse(ssp, "error", "400")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Invalid OpenRTB %s: %v", version, err)))
		return
	}

	// 6. Shortlist DSPs
	candidates := h.PartnersManager.GetDSPsByTenant(int(ssp.TenantID))
	selectedDSPs := partners.ShortlistDSPs(&bidReq, candidates, ssp.SSPIdentifier, 5, bidReq.TMax)

	if len(selectedDSPs) == 0 {
		h.recordSSPResponse(ssp, "no_bid", "204")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 6. Parallel Fan-out
	auctionCtx, cancel := context.WithTimeout(r.Context(), time.Duration(bidReq.TMax)*time.Millisecond)
	defer cancel()

	bidChan := make(chan bidResult, len(selectedDSPs))
	var wg sync.WaitGroup

	for _, dsp := range selectedDSPs {
		wg.Add(1)
		go func(d partners.DSPInventory) {
			defer wg.Done()

			// Metrics: Record DSP Request
			partners.DSPRequestCounter.WithLabelValues(d.DSPInventoryIdentifier, d.TenantIdentifier, d.DSPIdentifier).Inc()

			dspBidReq := endpoints.GetDspBidRequest(&bidReq, *ssp, d, h.GlobalASI)
			dspBody, _ := json.Marshal(dspBidReq)

			if isSampled {
				bidLogger.LogDSP(d.DSPInventoryIdentifier, dspBody, "REQ")
			}

			start := time.Now()
			resp, rawBody, err := h.callDSP(auctionCtx, d, dspBody)
			latency := time.Since(start).Seconds()

			if isSampled {
				bidLogger.LogDSP(d.DSPInventoryIdentifier, rawBody, "RESP")
			}

			// Metrics: Record DSP Response and Latency
			status := "nobid"
			httpCode := "204"
			if err == nil {
				status = "bid"
				if resp.NBR != nil {
					status = "nobid"
				} else if len(resp.SeatBid) == 0 {
					status = "nobid"
				}
				httpCode = "200"

				bidChan <- bidResult{
					resp:        resp,
					dsp:         d,
					reqBody:     dspBody,
					dspRespBody: rawBody,
				}
			} else {
				status = "error"
				httpCode = "5xx" // Default
				if respErr, ok := err.(partners.HTTPError); ok {
					if respErr.StatusCode >= 400 && respErr.StatusCode < 500 {
						httpCode = "4xx"
					} else {
						httpCode = "5xx"
					}
				}
				logger.Errorf("DSP %s call failed: %v", d.DSPIdentifier, err)
			}
			partners.DSPResponseCounter.WithLabelValues(d.DSPInventoryIdentifier, d.TenantIdentifier, d.DSPIdentifier, status, httpCode).Inc()
			partners.DSPLatencyHistogram.WithLabelValues(d.DSPInventoryIdentifier, d.TenantIdentifier, d.DSPIdentifier).Observe(latency)
		}(dsp)
	}

	go func() {
		wg.Wait()
		close(bidChan)
	}()

	// 7. Impression-Level Winner Selection
	winners := make(map[string]*bidResult)
	targetedDSPs := make(map[int]bidResult)
	for _, d := range selectedDSPs {
		targetedDSPs[int(d.DSPID)] = bidResult{dsp: d}
	}

	// Build exact impression mapping for O(1) validation lookups
	impMap := make(map[string]*openrtb2.Imp, len(bidReq.Imp))
	for i := range bidReq.Imp {
		impMap[bidReq.Imp[i].ID] = &bidReq.Imp[i]
	}

	for res := range bidChan {
		resCopy := res
		t := targetedDSPs[int(resCopy.dsp.DSPID)]
		t.resp = resCopy.resp
		t.dspRespBody = resCopy.dspRespBody
		t.reqBody = resCopy.reqBody
		targetedDSPs[int(resCopy.dsp.DSPID)] = t

		if !endpoints.ApplyExchangeMargin(resCopy.resp, impMap, bidReq.BCat, resCopy.dsp) {
			continue
		}

		for _, sb := range resCopy.resp.SeatBid {
			for i := range sb.Bid {
				bid := &sb.Bid[i]

				// Basic Validation Checks
				imp, isValidImp := impMap[bid.ImpID]
				if !isValidImp {
					partners.DSPValidationFailedCounter.WithLabelValues(resCopy.dsp.DSPInventoryIdentifier, resCopy.dsp.TenantIdentifier, resCopy.dsp.DSPIdentifier).Inc()
					continue // DSP bid on unrecognized impression ID
				}

				// Price Floor Validation
				if bid.Price <= 0 || bid.Price < imp.BidFloor {
					partners.DSPValidationFailedCounter.WithLabelValues(resCopy.dsp.DSPInventoryIdentifier, resCopy.dsp.TenantIdentifier, resCopy.dsp.DSPIdentifier).Inc()
					continue // Bid violates the specific impression bidfloor
				}

				// Payload Verification
				if bid.AdM == "" && bid.NURL == "" {
					partners.DSPValidationFailedCounter.WithLabelValues(resCopy.dsp.DSPInventoryIdentifier, resCopy.dsp.TenantIdentifier, resCopy.dsp.DSPIdentifier).Inc()
					continue // Empty creative payload
				}

				// Creative ID Checks
				if bid.CrID == "" && bid.AdID == "" {
					partners.DSPValidationFailedCounter.WithLabelValues(resCopy.dsp.DSPInventoryIdentifier, resCopy.dsp.TenantIdentifier, resCopy.dsp.DSPIdentifier).Inc()
					continue // Missing Creative IDs
				}

				currentWinner, exists := winners[bid.ImpID]
				if !exists || bid.Price > getBidPrice(currentWinner, bid.ImpID) {
					winners[bid.ImpID] = &resCopy
				}
			}
		}
	}

	if len(winners) == 0 {
		h.recordSSPResponse(ssp, "no_bid", "204")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 8. Build SSP Response
	finalResp := &openrtb2.BidResponse{
		ID:    bidReq.ID,
		BidID: fmt.Sprintf("pbs-%d", time.Now().UnixNano()),
		Cur:   "USD",
	}

	os, osv, country, carrier, deviceType, domain, bundle, _, _, _, _, _, _, _, _, _, _, _, _ := h.extractContext(&bidReq)

	for impID, win := range winners {
		var bestBid *openrtb2.Bid
		var bestSeat string
		for _, sb := range win.resp.SeatBid {
			for i := range sb.Bid {
				if sb.Bid[i].ImpID == impID {
					if bestBid == nil || sb.Bid[i].Price > bestBid.Price {
						bestBid = &sb.Bid[i]
						bestSeat = sb.Seat
					}
				}
			}
		}

		if bestBid == nil {
			continue
		}

		marginMultiplier := endpoints.GetMarginMultiplier(win.dsp)
		dspPrice := bestBid.Price / marginMultiplier

		imp := impMap[impID]
		adType, adSize := h.getAdDimensions(bestBid, imp)
		adTypeStr := "unknown"
		switch adType {
		case 1: adTypeStr = "banner"
		case 2: adTypeStr = "video"
		case 3: adTypeStr = "native"
		case 4: adTypeStr = "audio"
		}

		tck := endpoints.TrackingConfig{
			ExternalURL:   "http://win.event.cdapp.com:11000",
			AccountID:     fmt.Sprintf("%d", ssp.SSPInventoryID),
			Timestamp:     time.Now().UnixMilli(),
			Integration:   "auction_2_5",
			AuctionID:     bidReq.ID,
			Seat:          bestSeat,
			OS:            os,
			OSV:           osv,
			Country:       country,
			Carrier:       carrier,
			DeviceType:    deviceType,
			SiteAppDomain: domain,
			BundleID:      bundle,
			AdType:        adTypeStr,
			AdSize:        adSize,
			DSPCurrency:   strings.ToUpper(win.resp.Cur),
		}
		if len(tck.DSPCurrency) != 3 {
			tck.DSPCurrency = "USD"
		}

		endpoints.TransformWinningBid(bestBid, *ssp, win.dsp, dspPrice, imp.BidFloor, tck)

		// Metrics: Record Financials (Impression level)
		partners.ExchangeRevenueCounter.WithLabelValues(
			ssp.SSPInventoryIdentifier,
			win.dsp.DSPInventoryIdentifier,
			ssp.TenantIdentifier,
			ssp.SSPIdentifier,
			win.dsp.DSPIdentifier,
		).Add(dspPrice)

		partners.ExchangeSpentCounter.WithLabelValues(
			ssp.SSPInventoryIdentifier,
			win.dsp.DSPInventoryIdentifier,
			ssp.TenantIdentifier,
			ssp.SSPIdentifier,
			win.dsp.DSPIdentifier,
		).Add(bestBid.Price)

		partners.ExchangeProfitCounter.WithLabelValues(
			ssp.SSPInventoryIdentifier,
			win.dsp.DSPInventoryIdentifier,
			ssp.TenantIdentifier,
			ssp.SSPIdentifier,
			win.dsp.DSPIdentifier,
		).Add(dspPrice - bestBid.Price)

		finalResp.SeatBid = append(finalResp.SeatBid, openrtb2.SeatBid{
			Seat: win.dsp.DSPIdentifier,
			Bid:  []openrtb2.Bid{*bestBid},
		})
	}

	// Metrics: Record Successful SSP Response
	h.recordSSPResponse(ssp, "ok", "200")

	respBody, _ := json.Marshal(finalResp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)

	// 9. Simplified Winner-Only Logging (One log per Impression)
	h.logWinners(ssp, &bidReq, body, winners, impMap)
}

func (h *AuctionHandler) logWinners(ssp *partners.SSPInventory, bidReq *openrtb2.BidRequest, sspReqBody []byte, winners map[string]*bidResult, impMap map[string]*openrtb2.Imp) {
	bidLogger := logging.GetBidLogger()
	if bidLogger == nil {
		return
	}

	os, osv, country, carrier, deviceType, domain, bundle, ip, ua, ifa, gdprConsent, region, city, zip, language, make, model, connType, sspCur := h.extractContext(bidReq)

	for impID, win := range winners {
		// Find the winning bid for this specific impression in the win result
		var bestBid *openrtb2.Bid
		var bestSeat string
		for _, sb := range win.resp.SeatBid {
			for i := range sb.Bid {
				if sb.Bid[i].ImpID == impID {
					if bestBid == nil || sb.Bid[i].Price > bestBid.Price {
						bestBid = &sb.Bid[i]
						bestSeat = sb.Seat
					}
				}
			}
		}

		if bestBid == nil {
			continue // Should not happen given winners map logic
		}

		event := logging.GetEventFromPool()
		event.TenantId = uint32(ssp.TenantID)
		event.SspPartnerId = uint32(ssp.SSPID)
		event.SspInventoryId = uint32(ssp.SSPInventoryID)
		event.SspPartnerAuctionId = bidReq.ID
		event.DspPartnerId = uint32(win.dsp.DSPID)
		event.DspInventoryId = uint32(win.dsp.DSPInventoryID)
		event.RawBidRequest = sspReqBody
		event.Os = os
		event.Osv = osv
		event.Country = country
		event.Carrier = carrier
		event.Ip = ip
		event.UserAgent = ua
		event.Ifa = ifa
		event.DeviceType = deviceType
		event.SiteAppDomain = domain
		event.BundleId = bundle
		event.GdprConsent = gdprConsent
		event.Timestamp = time.Now().UnixMilli()
		event.Hostname = h.Hostname
		event.Currency = win.resp.Cur
		if event.Currency == "" {
			event.Currency = "USD"
		}
		event.Region = region
		event.City = city
		event.Zip = zip
		event.Language = language
		event.DeviceMake = make
		event.DeviceModel = model
		event.ConnectionType = uint32(connType)
		event.SspCurrency = sspCur

		// Selection & Audit Details
		event.WinningBidId = bestBid.ID
		event.ImpId = bestBid.ImpID
		event.SeatId = bestSeat
		event.CreativeId = bestBid.CrID
		event.DealId = bestBid.DealID
		event.IsPmp = bestBid.DealID != ""
		if len(bestBid.ADomain) > 0 {
			event.AdDomain = bestBid.ADomain[0]
		}

		// Financials
		event.DspPrice = bestBid.Price / endpoints.GetMarginMultiplier(win.dsp)
		event.SspPrice = bestBid.Price

		if imp, ok := impMap[bestBid.ImpID]; ok {
			event.BidRequestPrice = imp.BidFloor
			adType, adSize := h.getAdDimensions(bestBid, imp)
			event.AdType = adType
			event.AdSize = adSize

			if imp.Video != nil {
				if len(imp.Video.PlaybackMethod) > 0 {
					event.PlaybackMethod = uint32(imp.Video.PlaybackMethod[0])
				}
				event.VideoPlacement = uint32(imp.Video.Placement)
			}
		}

		// Source Environment
		if bidReq.App != nil {
			event.Source = &generated.AuctionEvent_App{
				App: &generated.App{
					Id:     bidReq.App.ID,
					Name:   bidReq.App.Name,
					Bundle: bidReq.App.Bundle,
					Domain: bidReq.App.Domain,
				},
			}
		} else if bidReq.Site != nil {
			event.Source = &generated.AuctionEvent_Web{
				Web: &generated.Web{
					Domain: bidReq.Site.Domain,
					Page:   bidReq.Site.Page,
				},
			}
		}

		// Response body for this winner
		respBinary, _ := json.Marshal(win.resp)
		event.SspDspResponse = respBinary

		// Log Proto
		bidLogger.Log(event)

		// Verbose Logging (Only for winners)
		if win.reqBody != nil {
			bidLogger.LogDSP(win.dsp.DSPInventoryIdentifier, win.reqBody, "REQ")
		}
		if win.dspRespBody != nil {
			bidLogger.LogDSP(win.dsp.DSPInventoryIdentifier, win.dspRespBody, "RESP")
		}
	}

	// Always Log SSP Request/Response globally once winners are set
	// Note: We might want a separate consolidated response log here or just use the winning events
	// but the user said "ssp and targeting details same all 5 logs".
}

func (h *AuctionHandler) callDSP(ctx context.Context, dsp partners.DSPInventory, body []byte) (*openrtb2.BidResponse, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", dsp.EndpointURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.HttpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, respBody, partners.HTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("DSP returned status %d", resp.StatusCode),
		}
	}

	var bidResp openrtb2.BidResponse
	if err := json.Unmarshal(respBody, &bidResp); err != nil {
		return nil, respBody, err
	}

	return &bidResp, respBody, nil
}

func getBidPrice(res *bidResult, impID string) float64 {
	if res == nil || res.resp == nil {
		return 0.0
	}
	for _, sb := range res.resp.SeatBid {
		for _, bid := range sb.Bid {
			if bid.ImpID == impID {
				return bid.Price
			}
		}
	}
	return 0.0
}

func (h *AuctionHandler) extractContext(bidReq *openrtb2.BidRequest) (os, osv, country, carrier string, deviceType uint32, domain, bundle, ip, ua, ifa, gdprConsent, region, city, zip, language, make, model string, connType int, sspCur string) {
	if len(bidReq.Cur) > 0 && len(bidReq.Cur[0]) == 3 {
		sspCur = strings.ToUpper(bidReq.Cur[0])
	} else {
		sspCur = "USD"
	}
	if bidReq.Device != nil {
		os = strings.ToLower(bidReq.Device.OS)
		osv = strings.ToLower(bidReq.Device.OSV)
		carrier = strings.ToLower(bidReq.Device.Carrier)
		ip = bidReq.Device.IP
		ua = bidReq.Device.UA
		ifa = bidReq.Device.IFA
		language = bidReq.Device.Language
		make = bidReq.Device.Make
		model = bidReq.Device.Model
		connType := 0
		if bidReq.Device.ConnectionType != nil {
			connType = int(*bidReq.Device.ConnectionType)
		}
		if bidReq.Device.DeviceType > 0 {
			deviceType = uint32(bidReq.Device.DeviceType)
		}
		if bidReq.Device.Geo != nil {
			country = strings.ToUpper(bidReq.Device.Geo.Country)
			region = strings.ToUpper(bidReq.Device.Geo.Region)
			city = bidReq.Device.Geo.City
			zip = bidReq.Device.Geo.ZIP
		}
		return os, osv, country, carrier, deviceType, domain, bundle, ip, ua, ifa, gdprConsent, region, city, zip, language, make, model, connType, sspCur
	}
	if bidReq.App != nil {
		domain = strings.ToLower(bidReq.App.Domain)
		bundle = strings.ToLower(bidReq.App.Bundle)
	} else if bidReq.Site != nil {
		domain = strings.ToLower(bidReq.Site.Domain)
	}

	if bidReq.User != nil && len(bidReq.User.Ext) > 0 {
		if val, err := jsonparser.GetString(bidReq.User.Ext, "consent"); err == nil {
			gdprConsent = val
		}
	}
	return
}

func (h *AuctionHandler) getAdDimensions(bid *openrtb2.Bid, imp *openrtb2.Imp) (adType uint32, adSize string) {
	if imp != nil {
		if imp.Banner != nil {
			adType = 1
		} else if imp.Video != nil {
			adType = 2
		} else if imp.Native != nil {
			adType = 3
		} else if imp.Audio != nil {
			adType = 4
		}

		if bid.W > 0 && bid.H > 0 {
			adSize = fmt.Sprintf("%dx%d", bid.W, bid.H)
		} else if imp.Banner != nil && imp.Banner.W != nil && imp.Banner.H != nil {
			adSize = fmt.Sprintf("%dx%d", *imp.Banner.W, *imp.Banner.H)
		}
	}
	return
}

func (h *AuctionHandler) recordSSPResponse(ssp *partners.SSPInventory, status string, code string) {
	sspInvId := "unknown"
	tenantId := "unknown"
	sspId := "unknown"
	if ssp != nil {
		sspInvId = ssp.SSPInventoryIdentifier
		tenantId = ssp.TenantIdentifier
		sspId = ssp.SSPIdentifier
	}

	// Grouping 4xx and 5xx codes
	if len(code) > 0 {
		if code[0] == '4' {
			code = "4xx"
		} else if code[0] == '5' {
			code = "5xx"
		}
	}

	partners.SSPResponseCounter.WithLabelValues(sspInvId, tenantId, sspId, status, code).Inc()
}

func getDeviceTypeName(dt int) string {
	switch dt {
	case 1:
		return "Mobile/Tablet"
	case 2:
		return "Personal Computer"
	case 3:
		return "Connected TV"
	case 4:
		return "Phone"
	case 5:
		return "Tablet"
	case 6:
		return "Connected Device"
	case 7:
		return "Set Top Box"
	default:
		return "Unknown"
	}
}
