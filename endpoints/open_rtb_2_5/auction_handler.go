package open_rtb_2_5

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
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
	partners.SSPRequestCounter.WithLabelValues(ssp.PrometheusIdentifier, strconv.Itoa(ssp.TenantID)).Inc()

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

	// 5. Check Tmax
	if bidReq.TMax <= 120 {
		partners.AuctionCounter.WithLabelValues("rejected_tmax").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 6. Shortlist DSPs
	candidates := h.PartnersManager.GetDSPsByTenant(ssp.TenantID)
	selectedDSPs := partners.ShortlistDSPs(&bidReq, candidates, 5)

	if len(selectedDSPs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 7. Conduct Auction (Fan-out)
	auctionCtx, cancel := context.WithTimeout(context.Background(), time.Duration(bidReq.TMax)*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	bidChan := make(chan *openrtb2.BidResponse, len(selectedDSPs))

	for _, dsp := range selectedDSPs {
		wg.Add(1)
		go func(d partners.DSPInventory) {
			defer wg.Done()

			// Mark fan-out to DSP in Prometheus
			partners.DSPRequestCounter.WithLabelValues(d.PrometheusIdentifier, strconv.Itoa(d.TenantID)).Inc()

			resp, err := h.callDSP(auctionCtx, d, body)
			if err != nil {
				return
			}
			bidChan <- resp
		}(dsp)
	}

	go func() {
		wg.Wait()
		close(bidChan)
	}()

	// 8. Collect and Select Best Bid
	var bestResponse *openrtb2.BidResponse
	var maxPrice float64

	for resp := range bidChan {
		for _, sb := range resp.SeatBid {
			for _, bid := range sb.Bid {
				if bid.Price > maxPrice {
					maxPrice = bid.Price
					bestResponse = resp
				}
			}
		}
	}

	if bestResponse == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 9. Return Response
	respBody, _ := json.Marshal(bestResponse)
	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
	partners.AuctionCounter.WithLabelValues("ok").Inc()
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
