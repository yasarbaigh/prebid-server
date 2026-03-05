package openrtb_2_5

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/endpoints"
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

type AuctionHandler struct {
	PartnersManager *partners.Manager
	HttpClient      *http.Client
}

func NewAuctionHandler(pm *partners.Manager) *AuctionHandler {
	return &AuctionHandler{
		PartnersManager: pm,
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
	// 1. Check AdServing flag
	cfg := h.PartnersManager.GetConfig()
	if cfg == nil || !cfg.AdServing {
		partners.AuctionCounter.WithLabelValues("rejected_adserving_disabled").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Get account_code from query params
	accountCode := r.URL.Query().Get("account_code")
	if accountCode == "" {
		http.Error(w, "Missing account_code", http.StatusUnauthorized)
		return
	}

	// 3. Identify SSP
	ssp, ok := h.PartnersManager.GetSSPByInventoryCode(accountCode)
	if !ok {
		http.Error(w, "Invalid account_code", http.StatusUnauthorized)
		return
	}

	// Mark request from SSP in Prometheus
	partners.SSPRequestCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier).Inc()

	// 4. Read and Parse BidRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var bidReq openrtb2.BidRequest
	if err := json.Unmarshal(body, &bidReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 5. Pre-Check Validator (Pre-Auction Validation)
	if err := endpoints.ValidateBidRequest(&bidReq); err != nil {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "400").Inc()
		http.Error(w, fmt.Sprintf("Invalid Bid Request: %v", err), http.StatusBadRequest)
		return
	}

	// Log SSP Request
	if bidLogger := logging.GetBidLogger(); bidLogger != nil {
		bidLogger.LogSSP(uint32(ssp.SSPID), body, "REQ")
	}

	// 5. Check Tmax
	if bidReq.TMax <= 120 {
		partners.AuctionCounter.WithLabelValues("rejected_tmax").Inc()
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "error", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 6. Shortlist DSPs
	candidates := h.PartnersManager.GetDSPsByTenant(ssp.TenantID)
	selectedDSPs := partners.ShortlistDSPs(&bidReq, candidates, 5)

	if len(selectedDSPs) == 0 {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 7. Conduct Auction (Fan-out)
	auctionCtx, cancel := context.WithTimeout(context.Background(), time.Duration(bidReq.TMax)*time.Millisecond)
	defer cancel()

	// 8. Collect and Select Best Bid
	type bidResult struct {
		resp *openrtb2.BidResponse
		dsp  partners.DSPInventory
	}
	bidChan := make(chan bidResult, len(selectedDSPs))
	var wg sync.WaitGroup

	for _, dsp := range selectedDSPs {
		wg.Add(1)
		go func(d partners.DSPInventory) {
			defer wg.Done()

			// 7.1 Calculate per-DSP BidRequest (Uplift Floors by Margin + SChain)
			dspBidReq := endpoints.GetDspBidRequest(&bidReq, *ssp, d)
			dspBody, _ := json.Marshal(dspBidReq)

			// Mark fan-out to DSP in Prometheus
			partners.DSPRequestCounter.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier).Inc()

			// Log DSP Request (specific to this DSP's floor)
			if bidLogger := logging.GetBidLogger(); bidLogger != nil {
				bidLogger.LogDSP(uint32(d.DSPID), dspBody, "REQ")
			}

			start := time.Now()
			resp, err := h.callDSP(auctionCtx, d, dspBody)
			latency := time.Since(start).Seconds()

			// Record Latency
			partners.DSPLatencyHistogram.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier).Observe(latency)

			if err != nil {
				partners.DSPResponseCounter.WithLabelValues(d.PrometheusIdentifier, d.TenantIdentifier, d.DSPIdentifier, "error").Inc()
				return
			}

			// Log DSP Response
			if bidLogger := logging.GetBidLogger(); bidLogger != nil {
				respBody, _ := json.Marshal(resp)
				bidLogger.LogDSP(uint32(d.DSPID), respBody, "RESP")
			}

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
					// Extract status code from error if possible, or just mark as error
					httpCode = "5xx" // Generic for HTTP errors
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

			bidChan <- bidResult{resp: resp, dsp: d}
		}(dsp)
	}

	go func() {
		wg.Wait()
		close(bidChan)
	}()

	var bestResult *bidResult
	var maxPrice float64

	for res := range bidChan {
		for _, sb := range res.resp.SeatBid {
			for _, bid := range sb.Bid {
				if bid.Price > maxPrice {
					maxPrice = bid.Price
					resCopy := res
					bestResult = &resCopy
				}
			}
		}
	}

	if bestResult == nil {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 8.5 Apply Exchange Margin and Check SSP Bid Floor (Using price_handler.go helper)
	if !endpoints.ApplyExchangeMargin(bestResult.resp, &bidReq, bestResult.dsp) {
		partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "no_bid", "204").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 8.6 Record Profit Metrics
	// Extract final SSP Price (after margin) and original DSP Price for accounting
	var sspPrice, dspPrice float64
	marginMultiplier := endpoints.GetMarginMultiplier(bestResult.dsp)
	if len(bestResult.resp.SeatBid) > 0 && len(bestResult.resp.SeatBid[0].Bid) > 0 {
		// Calculate the original DSP price based on the current SSP price and margin
		sspPrice = bestResult.resp.SeatBid[0].Bid[0].Price
		dspPrice = sspPrice / marginMultiplier
	}

	partners.ExchangeRevenueCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(dspPrice)
	partners.ExchangeSpentCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(sspPrice)
	partners.ExchangeProfitCounter.WithLabelValues(ssp.SSPIdentifier, bestResult.dsp.DSPIdentifier, ssp.TenantIdentifier).Add(dspPrice - sspPrice)

	// 8.7 Transform Winning Bid (Apply custom NURL with AES encryption and AdM tracking)
	for i := range bestResult.resp.SeatBid {
		for j := range bestResult.resp.SeatBid[i].Bid {
			bid := &bestResult.resp.SeatBid[i].Bid[j]
			endpoints.TransformWinningBid(bid, *ssp, bestResult.dsp)
		}
	}

	// 9. Log Winning Event
	if bidLogger := logging.GetBidLogger(); bidLogger != nil {
		event := &generated.AuctionEvent{
			TenantId:            uint32(ssp.TenantID),
			SspPartnerId:        uint32(ssp.SSPID),
			SspInventoryId:      uint32(ssp.SSPInventoryID),
			SspPartnerAuctionId: bidReq.ID,
			DspPartnerId:        uint32(bestResult.dsp.DSPID),
			DspInventoryId:      uint32(bestResult.dsp.DSPInventoryID),
			DspPrice:            maxPrice,
			RawBidRequest:       body,
		}

		// Set bid floor if available
		if len(bidReq.Imp) > 0 {
			event.BidRequestPrice = bidReq.Imp[0].BidFloor
		}

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

		// Raw DSP Response
		respBody, _ := json.Marshal(bestResult.resp)
		event.RawDspResponse = respBody

		bidLogger.Log(event)
	}

	// 10. Return Response
	respBody, _ := json.Marshal(bestResult.resp)

	// Log SSP Response
	if bidLogger := logging.GetBidLogger(); bidLogger != nil {
		bidLogger.LogSSP(uint32(ssp.SSPID), respBody, "RESP")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
	partners.AuctionCounter.WithLabelValues("ok").Inc()
	partners.SSPResponseCounter.WithLabelValues(ssp.PrometheusIdentifier, ssp.TenantIdentifier, ssp.SSPIdentifier, "ok", "200").Inc()
}

func (h *AuctionHandler) callDSP(ctx context.Context, dsp partners.DSPInventory, body []byte) (*openrtb2.BidResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", dsp.EndpointURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DSP returned status %d", resp.StatusCode)
	}

	var bidResp openrtb2.BidResponse
	if err := json.NewDecoder(resp.Body).Decode(&bidResp); err != nil {
		return nil, err
	}

	return &bidResp, nil
}
