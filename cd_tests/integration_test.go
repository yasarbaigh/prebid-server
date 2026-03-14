package cd_tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/endpoints/openrtb_2_5"
	"github.com/prebid/prebid-server/v3/partners"
)

func TestIntegration_AuctionHandlerSuccess(t *testing.T) {
	// Mock DSP Server
	dspServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openrtb2.BidResponse{
			ID: "test-resp",
			SeatBid: []openrtb2.SeatBid{
				{
					Seat: "test-seat",
					Bid: []openrtb2.Bid{
						{
							ID:    "test-bid",
							ImpID: "test-imp",
							Price: 2.5,
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer dspServer.Close()

	cfg := partners.PartnersConfig{
		AdServing: true,
		ASI:       "test-asi.com",
		SSPInventories: []partners.SSPInventory{
			{
				InventoryCode: "test-ssp",
				TenantID:      1,
				Status:        "Active",
			},
		},
		DSPInventories: []partners.DSPInventory{
			{
				TenantID:    1,
				Status:      "Active",
				EndpointURL: dspServer.URL,
				DSPID:       10,
				Margin:      10,
			},
		},
	}

	cfgBytes, _ := json.Marshal(cfg)
	tmpFile, err := os.CreateTemp("", "partners*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(cfgBytes)
	tmpFile.Close()

	pm := partners.NewManager()
	if err := pm.Load(tmpFile.Name()); err != nil {
		t.Fatal(err)
	}

	handler := openrtb_2_5.NewAuctionHandler(pm)

	bidReq := openrtb2.BidRequest{
		ID:   "test-req",
		TMax: 500,
		Imp: []openrtb2.Imp{
			{
				ID: "test-imp",
				Banner: &openrtb2.Banner{
					W: openrtb2.Int64Ptr(300),
					H: openrtb2.Int64Ptr(250),
				},
				BidFloor: 1.0,
			},
		},
        Site: &openrtb2.Site{
            Page: "http://test.com",
            Publisher: &openrtb2.Publisher{
                ID: "pub-1",
            },
        },
        Device: &openrtb2.Device{
            UA: "test-ua",
            IP: "1.2.3.4",
        },
	}
	body, _ := json.Marshal(bidReq)

	req := httptest.NewRequest("POST", "/auction?c=test-ssp", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	handler.Handle(rr, req, httprouter.Params{})

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected %v, got %v with string: %s", http.StatusOK, status, rr.Body.String())
	}

	var resp openrtb2.BidResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
        if rr.Code == 200 {
		    t.Fatal(err)
        }
	}

	if rr.Code == 200 {
        if len(resp.SeatBid) == 0 || len(resp.SeatBid[0].Bid) == 0 {
            t.Fatal("Expected bids in response")
        }
    }
}
