package openrtb_2_5

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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

// RtbAuctionRequest is the custom request structure for this use-case
type RtbAuctionRequest struct {
	SSP        *partners.SSPInventory
	BidRequest *openrtb2.BidRequest
	DSPs       []partners.DSPInventory
}

var (
	json = jsoniter.ConfigCompatibleWithStandardLibrary
)

type AuctionHandler struct {
	PartnersManager *partners.Manager
	HttpClient      *http.Client
	GlobalASI       string
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
			},
		},
	}
}

func (h *AuctionHandler) Handle(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Panic Recovery to ensure the server never crashes and always returns 204 on catastrophe
	defer func() {
		if r := recover(); r != nil {
			// Log the panic details
			errDetail := fmt.Sprintf("PANIC RECOVERED: %v", r)
			logger.Errorf(errDetail)
			if bidLogger := logging.GetBidLogger(); bidLogger != nil {
				// We don't have sspID yet if it panics very early, but we try to log what we can
				bidLogger.LogSSP("SYSTEM_PANIC", []byte(errDetail), "CRITICAL_ERROR")
			}
			// Always return 204 No Content to the SSP on any panic
			w.WriteHeader(http.StatusNoContent)
		}
	}()

	// 1. Check AdServing and Config Health (Strict 10m TS check)
	if !h.PartnersManager.IsHealthy() || !h.PartnersManager.GetConfig().AdServing {
		partners.AuctionCounter.WithLabelValues("rejected_unhealthy_config").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Get identification code from query params (c)
	accountCode := r.URL.Query().Get("c")

	if accountCode == "" {
		partners.AuctionCounter.WithLabelValues("invalid_request_missing_identification").Inc()
		http.Error(w, "Missing identification code (c)", http.StatusBadRequest)
		return
	}

	// 3. Identify SSP
	ssp, ok := h.PartnersManager.GetSSPByInventoryCode(accountCode)
	if !ok {
		partners.AuctionCounter.WithLabelValues("invalid_request_bad_identification").Inc()
		http.Error(w, "Invalid identification code", http.StatusBadRequest)
		return
	}

	// Mark request from SSP in Prometheus
	partners.SSPRequestCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier).Inc()

	// 4. Read Body with size limit (Anti-DoS / Memory Leak prevention)
	// Limit to 2MB as RTB requests are rarely larger
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// If body is too large, http.MaxBytesReader returns an error
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := endpoints.FastValidateBidRequest(body); err != nil {
		partners.AuctionCounter.WithLabelValues("invalid_request_fast_precheck").Inc()
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "400").Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 5. Full Parse BidRequest (Using jsoniter for speed)
	var bidReq openrtb2.BidRequest
	if err := json.Unmarshal(body, &bidReq); err != nil {
		partners.AuctionCounter.WithLabelValues("invalid_json").Inc()
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "400").Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 6. Pre-Check Validator (Pre-Auction Validation)
	if err := endpoints.ValidateBidRequest(&bidReq); err != nil {
		partners.AuctionCounter.WithLabelValues("invalid_request_validation").Inc()
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "400").Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Log SSP Request
	if bidLogger := logging.GetBidLogger(); bidLogger != nil {
		bidLogger.LogSSP(ssp.PrometheusIdentifier, body, "REQ")
	}

	// 7. Check Tmax
	if bidReq.TMax <= 120 {
		partners.AuctionCounter.WithLabelValues("rejected_tmax").Inc()
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 8. Shortlist DSPs
	candidates := h.PartnersManager.GetDSPsByTenant(ssp.TenantID)
	selectedDSPs := partners.ShortlistDSPs(&bidReq, candidates, 5)

	if len(selectedDSPs) == 0 {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	auctionCtx, cancel := context.WithTimeout(r.Context(), time.Duration(bidReq.TMax)*time.Millisecond)
	defer cancel()

	// 8. Collect and Select Best Bid
	type bidResult struct {
		resp        *openrtb2.BidResponse
		dsp         partners.DSPInventory
		reqBody     []byte
		dspRespBody []byte
	}
	bidChan := make(chan bidResult, len(selectedDSPs))
	var wg sync.WaitGroup

	for _, dsp := range selectedDSPs {
		wg.Add(1)
		go func(d partners.DSPInventory) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("CRITICAL: Panic in DSP %s fan-out: %v", d.PrometheusIdentifier, r)
					partners.DSPResponseCounter.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier, "panic", "500").Inc()
				}
			}()

			// 7.1 Calculate per-DSP BidRequest (Uplift Floors by Margin + SChain)
			dspBidReq := endpoints.GetDspBidRequest(&bidReq, *ssp, d, h.GlobalASI)
			dspBody, _ := json.Marshal(dspBidReq)

			// Mark fan-out to DSP in Prometheus
			partners.DSPRequestCounter.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier).Inc()

			start := time.Now()
			resp, rawBody, err := h.callDSP(auctionCtx, d, dspBody)
			latency := time.Since(start).Seconds()

			// Record Latency
			partners.DSPLatencyHistogram.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier).Observe(latency)

			// Check if it's a "No Bid" (empty seatbid or zero bids)
			hasBid := false
			if resp != nil {
				for _, sb := range resp.SeatBid {
					if len(sb.Bid) > 0 {
						hasBid = true
						break
					}
				}
			}

			status := "error"
			httpCode := "500"
			if err != nil {
				if strings.Contains(err.Error(), "status") {
					httpCode = "5xx"
				}
			} else {
				status = "nobid"
				httpCode = "204"
				if hasBid {
					status = "bid"
					httpCode = "200"
				}
			}
			partners.DSPResponseCounter.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier, status, httpCode).Inc()

			if err != nil {
				return // Do not send to bidChan if there was an error
			}

			bidChan <- bidResult{
				resp:        resp,
				dsp:         d,
				reqBody:     dspBody,
				dspRespBody: rawBody,
			}
		}(dsp)
	}

	go func() {
		wg.Wait()
		close(bidChan)
	}()

	var bestResult *bidResult
	var winningBid *openrtb2.Bid
	var winningSeat string
	var maxPrice float64

	for res := range bidChan {
		if res.resp == nil {
			continue
		}
		for _, sb := range res.resp.SeatBid {
			for i := range sb.Bid {
				bid := &sb.Bid[i]
				if bid.Price > maxPrice {
					maxPrice = bid.Price
					// Capture the best result but keep it locally since we're in range
					bestResult = &bidResult{
						resp:        res.resp,
						dsp:         res.dsp,
						reqBody:     res.reqBody,
						dspRespBody: res.dspRespBody,
					}
					winningBid = bid
					winningSeat = sb.Seat
				}
			}
		}
	}

	if bestResult == nil {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 9. Apply Exchange Margin and Check SSP Bid Floor (Using price_handler.go helper)
	if !endpoints.ApplyExchangeMargin(bestResult.resp, &bidReq, bestResult.dsp) {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 10. Record Profit Metrics
	// Extract final SSP Price (after margin) and original DSP Price for accounting
	var sspPrice, dspPrice float64
	marginMultiplier := endpoints.GetMarginMultiplier(bestResult.dsp)
	if winningBid != nil {
		// Calculate the original DSP price based on the current SSP price and margin
		sspPrice = winningBid.Price
		dspPrice = sspPrice / marginMultiplier
	}

	partners.ExchangeRevenueCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(dspPrice)
	partners.ExchangeSpentCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(sspPrice)
	partners.ExchangeProfitCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(dspPrice - sspPrice)

	// Pre-compute Impression map to fix O(N^2) loop overhead
	impMap := make(map[string]*openrtb2.Imp, len(bidReq.Imp))
	for i := range bidReq.Imp {
		impMap[bidReq.Imp[i].ID] = &bidReq.Imp[i]
	}

	// 11. Pre-compute common dimensions for tracking and logging (Performance Optimization)
	var os, osv, country, carrier, deviceType, domain, bundle, ip, ua, ifa, gdprConsent string
	if bidReq.Device != nil {
		os = bidReq.Device.OS
		osv = bidReq.Device.OSV
		carrier = bidReq.Device.Carrier
		ip = bidReq.Device.IP
		ua = bidReq.Device.UA
		ifa = bidReq.Device.IFA
		if bidReq.Device.DeviceType > 0 {
			deviceType = getDeviceTypeName(int(bidReq.Device.DeviceType))
		}
		if bidReq.Device.Geo != nil {
			country = bidReq.Device.Geo.Country
		}
	}
	if bidReq.App != nil {
		domain = bidReq.App.Domain
		bundle = bidReq.App.Bundle
	} else if bidReq.Site != nil {
		domain = bidReq.Site.Domain
	}

	// Efficiently extract GDPR consent using jsonparser
	if bidReq.User != nil && len(bidReq.User.Ext) > 0 {
		if val, err := jsonparser.GetString(bidReq.User.Ext, "consent"); err == nil {
			gdprConsent = val
		}
	}
	if gdprConsent == "" && bidReq.Regs != nil && len(bidReq.Regs.Ext) > 0 {
		if val, err := jsonparser.GetString(bidReq.Regs.Ext, "gdpr_consent"); err == nil {
			gdprConsent = val
		}
	}

	// 12. Transform Winning Bid (Apply custom NURL with AES encryption and AdM tracking)
	for i := range bestResult.resp.SeatBid {
		for j := range bestResult.resp.SeatBid[i].Bid {
			bid := &bestResult.resp.SeatBid[i].Bid[j]

			var floor float64
			imp, ok := impMap[bid.ImpID]
			if ok {
				floor = imp.BidFloor
			}

			// Determine Ad Type and Size for this specific bid
			var adType, adSize string
			if imp != nil {
				if imp.Banner != nil {
					adType = "banner"
				} else if imp.Video != nil {
					adType = "video"
				} else if imp.Native != nil {
					adType = "native"
				} else if imp.Audio != nil {
					adType = "audio"
				}

				if bid.W > 0 && bid.H > 0 {
					adSize = fmt.Sprintf("%dx%d", bid.W, bid.H)
				} else if imp.Banner != nil && imp.Banner.W != nil && imp.Banner.H != nil {
					adSize = fmt.Sprintf("%dx%d", *imp.Banner.W, *imp.Banner.H)
				}
			}

			tck := endpoints.TrackingConfig{
				ExternalURL:   "http://win.event.cdapp.com:11000",
				AccountID:     fmt.Sprintf("%d", ssp.SSPInventoryID),
				Timestamp:     time.Now().UnixMilli(),
				Integration:   "auction_2_5",
				AuctionID:     bidReq.ID,
				Seat:          bestResult.resp.SeatBid[i].Seat,
				OS:            os,
				OSV:           osv,
				Country:       country,
				Carrier:       carrier,
				DeviceType:    deviceType,
				SiteAppDomain: domain,
				BundleID:      bundle,
				AdType:        adType,
				AdSize:        adSize,
			}

			// Restore original DSP price for tracking purposes
			dspPrice = bid.Price / marginMultiplier

			endpoints.TransformWinningBid(bid, *ssp, bestResult.dsp, dspPrice, floor, tck)
		}
	}

	// 13. Prepare Final Response Body
	respBody, _ := json.Marshal(bestResult.resp)

	// 14. Send Response to SSP IMMEDIATELY (Performance: Don't block on logging)
	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)

	// 15. Async/Off-thread Logging (BidLogger logic is already async via channels)
	bidLogger := logging.GetBidLogger()
	if bidLogger != nil {
		event := logging.GetEventFromPool()

		event.TenantId = uint32(ssp.TenantID)
		event.SspPartnerId = uint32(ssp.SSPID)
		event.SspInventoryId = uint32(ssp.SSPInventoryID)
		event.SspPartnerAuctionId = bidReq.ID
		event.DspPartnerId = uint32(bestResult.dsp.DSPID)
		event.DspInventoryId = uint32(bestResult.dsp.DSPInventoryID)
		event.DspPrice = dspPrice
		event.SspPrice = sspPrice
		event.RawBidRequest = body
		event.Os = os
		event.Osv = osv
		event.Carrier = carrier
		event.Ip = ip
		event.UserAgent = ua
		event.Ifa = ifa
		event.DeviceType = deviceType
		event.Country = country
		event.SiteAppDomain = domain
		event.BundleId = bundle
		event.GdprConsent = gdprConsent
		event.SspDspResponse = respBody

		// Set source (App vs Web)
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

		// Extract Ad Dimensions if winning bid exists
		if winningBid != nil {
			event.WinningBidId = winningBid.ID
			event.ImpId = winningBid.ImpID
			event.SeatId = winningSeat
			event.CreativeId = winningBid.CrID
			event.DealId = winningBid.DealID
			event.IsPmp = winningBid.DealID != ""
			if len(winningBid.ADomain) > 0 {
				event.AdDomain = winningBid.ADomain[0]
			}

			if imp, ok := impMap[winningBid.ImpID]; ok && imp != nil {
				if imp.Banner != nil {
					event.AdType = "banner"
				} else if imp.Video != nil {
					event.AdType = "video"
				} else if imp.Native != nil {
					event.AdType = "native"
				} else if imp.Audio != nil {
					event.AdType = "audio"
				}

				if winningBid.W > 0 && winningBid.H > 0 {
					event.AdSize = fmt.Sprintf("%dx%d", winningBid.W, winningBid.H)
				} else if imp.Banner != nil && imp.Banner.W != nil && imp.Banner.H != nil {
					event.AdSize = fmt.Sprintf("%dx%d", *imp.Banner.W, *imp.Banner.H)
				}
			}
		}

		if len(bidReq.Imp) > 0 {
			event.BidRequestPrice = bidReq.Imp[0].BidFloor
		}

		// Log All winner interactions in one place
		bidLogger.Log(event)

		// Log SSP interactions in Verbose (only if enabled)
		bidLogger.LogSSP(ssp.PrometheusIdentifier, body, "REQ")
		bidLogger.LogSSP(ssp.PrometheusIdentifier, respBody, "RESP")

		// Log Winning DSP interactions in Verbose (reqBody and raw dspRespBody)
		bidLogger.LogDSP(bestResult.dsp.PrometheusIdentifier, bestResult.reqBody, "REQ")
		bidLogger.LogDSP(bestResult.dsp.PrometheusIdentifier, bestResult.dspRespBody, "RESP")
	}

	partners.AuctionCounter.WithLabelValues("ok").Inc()
	partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "ok", "200").Inc()
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
		// Draining is essential for connection reuse in high throughput
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Memory Protection: Limit DSP response to 1MB
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, respBody, fmt.Errorf("DSP returned status %d", resp.StatusCode)
	}

	var bidResp openrtb2.BidResponse
	if err := json.Unmarshal(respBody, &bidResp); err != nil {
		logger.Errorf("Failed to decode DSP %s response: %v", dsp.PrometheusIdentifier, err)
		return nil, respBody, err
	}

	return &bidResp, respBody, nil
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
