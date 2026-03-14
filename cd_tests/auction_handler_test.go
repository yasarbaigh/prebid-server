package cd_tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/prebid-server/v3/endpoints/openrtb_2_5"
	"github.com/prebid/prebid-server/v3/partners"
)

func setupTestManager(t *testing.T) (*partners.Manager, string) {
	cfg := partners.PartnersConfig{
		AdServing: true,
		ASI:       "test-asi.com",
		SSPInventories: []partners.SSPInventory{
			{
				InventoryCode: "test-ssp-code",
				TenantID:      1,
				Status:        "Active",
			},
		},
		DSPInventories: []partners.DSPInventory{
			{
				TenantID: 1,
				Status:   "Active",
				EndpointURL: "http://mock-dsp/bid",
			},
		},
	}

	cfgBytes, _ := json.Marshal(cfg)
	tmpFile, err := os.CreateTemp("", "partners*.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Write(cfgBytes)
	tmpFile.Close()

	pm := partners.NewManager()
	if err := pm.Load(tmpFile.Name()); err != nil {
		t.Fatal(err)
	}

	return pm, tmpFile.Name()
}

func TestAuctionHandler_NoContentWhenAdServingDisabled(t *testing.T) {
	pm, tmpFile := setupTestManager(t)
	defer os.Remove(tmpFile)

	// disable ad serving
	cfg := pm.GetConfig()
	cfg.AdServing = false

	handler := openrtb_2_5.NewAuctionHandler(pm)

	req := httptest.NewRequest("POST", "/auction?c=test-ssp-code", nil)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req, httprouter.Params{})

	if status := rr.Code; status != http.StatusNoContent {
		t.Errorf("expected %v, got %v", http.StatusNoContent, status)
	}
}

func TestAuctionHandler_MissingIdentificationCode(t *testing.T) {
	pm, tmpFile := setupTestManager(t)
	defer os.Remove(tmpFile)

	handler := openrtb_2_5.NewAuctionHandler(pm)

	req := httptest.NewRequest("POST", "/auction", nil)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req, httprouter.Params{})

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected %v, got %v", http.StatusBadRequest, status)
	}
}

func TestAuctionHandler_InvalidJSON(t *testing.T) {
	pm, tmpFile := setupTestManager(t)
	defer os.Remove(tmpFile)

	handler := openrtb_2_5.NewAuctionHandler(pm)

	req := httptest.NewRequest("POST", "/auction?c=test-ssp-code", bytes.NewBuffer([]byte("{invalid-json")))
	rr := httptest.NewRecorder()

	handler.Handle(rr, req, httprouter.Params{})

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected %v, got %v", http.StatusBadRequest, status)
	}
}
