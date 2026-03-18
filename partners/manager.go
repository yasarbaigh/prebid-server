package partners

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/prebid/prebid-server/v3/logger"
)

type SSPInventory struct {
	Name                 string   `json:"name"`
	ID                   int      `json:"id"`
	InventoryName        string   `json:"inventory_name"`
	Status               string   `json:"status"`
	InventoryCode        string   `json:"inventory_code"`
	TenantIdentifier     string   `json:"tenant_identifier"`
	SSPIdentifier        string   `json:"ssp_identifier"`
	TenantID             int      `json:"tenant_id"`
	SSPID                int      `json:"ssp_id"`
	SSPInventoryID       int      `json:"ssp_inventory_id"`
	PrometheusID         string   `json:"prometheus_id"`
	PrometheusIdentifier string   `json:"prometheus_identifier"`
	AdFormats            []string `json:"ad_formats"`
	WinURL               string   `json:"win_url"`
	ImpTrackURL          string   `json:"imp_track_url"`
	ClickTrackURL        string   `json:"click_track_url"`
	WinBaseDmn           string   `json:"win_base_dmn"`
	AdmPriceTransparency bool     `json:"adm_price_transparency"`
	SChainNode           string   `json:"schain_node"` // Your exchange identity for this SSP
}

type DSPInventory struct {
	Name                 string   `json:"name"`
	DSPIdentifier        string   `json:"dsp_identifier"`
	EndpointName         string   `json:"endpoint_name"`
	EndpointURL          string   `json:"endpoint_url"`
	QPS                  int      `json:"qps"`
	Tmax                 int      `json:"tmax"`
	ID                   int      `json:"id"`
	InventoryCode        string   `json:"inventory_code"`
	Status               string   `json:"status"`
	MinBidFloor          string   `json:"min_bidfloor"`
	MaxBidFloor          string   `json:"max_bidfloor"`
	AdFormats            []string `json:"ad_formats"`
	Source               []string `json:"source"`
	Country              []string `json:"country"`
	CountryBlackList     []string `json:"country_black_list"`
	IABCategories        []string `json:"iab_categories"`
	BundleIDs            []string `json:"bundle_ids"`
	BundleIDsBlackList   []string `json:"bundle_ids_black_list"`
	SSPs                 []string `json:"ssps"`
	SSPsBlackList        []string `json:"ssps_black_list"`
	Publishers           []string `json:"publishers"`
	PublishersBlackList  []string `json:"publishers_black_list"`
	TenantIdentifier     string   `json:"tenant_identifier"`
	TenantID             int      `json:"tenant_id"`
	DSPID                int      `json:"dsp_id"`
	DSPInventoryID       int      `json:"dsp_inventory_id"`
	PrometheusID         string   `json:"prometheus_id"`
	PrometheusIdentifier string   `json:"prometheus_identifier"`
	AdmPriceTransparency bool     `json:"adm_price_transparency"`
	Margin               int      `json:"margin"`
	BidAdjustment        float64  `json:"bid_adjustment"` // Multiplier (e.g. 0.9)
	PricingAt            int      `json:"pricing_at"`
}

type PartnersConfig struct {
	SSPInventories []SSPInventory `json:"ssp_inventories"`
	DSPInventories []DSPInventory `json:"dsp_inventories"`
	AdServing      bool           `json:"ad_serving"`
	ASI            string         `json:"asi"`
	TS             int64          `json:"ts"` // Unix timestamp in seconds

	// Fast lookup maps (calculated during load)
	sspMap map[string]*SSPInventory
	dspMap map[int][]DSPInventory
}

type Manager struct {
	config atomic.Pointer[PartnersConfig]
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		m.config.Store(nil) // Clear config on failure to stop serving
		return fmt.Errorf("failed to read partners file: %v", err)
	}

	var cfg PartnersConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		m.config.Store(nil)
		return fmt.Errorf("failed to unmarshal partners config: %v", err)
	}

	// Strict TS Check: Stop serving if TS is older than 10 minutes
	now := time.Now().Unix()
	if cfg.TS == 0 || (now-cfg.TS) > 600 {
		m.config.Store(nil)
		return fmt.Errorf("partners config TS is stale or missing (TS: %d, Now: %d)", cfg.TS, now)
	}

	// Default PricingAt to 1 if not available
	for i := range cfg.DSPInventories {
		if cfg.DSPInventories[i].PricingAt == 0 {
			cfg.DSPInventories[i].PricingAt = 1
		}
	}

	// Build Lookup Maps for O(1) performance
	cfg.sspMap = make(map[string]*SSPInventory)
	for i := range cfg.SSPInventories {
		cfg.sspMap[cfg.SSPInventories[i].InventoryCode] = &cfg.SSPInventories[i]
	}

	cfg.dspMap = make(map[int][]DSPInventory)
	for i := range cfg.DSPInventories {
		// Only index Active DSPs
		if cfg.DSPInventories[i].Status == "Active" {
			cfg.dspMap[cfg.DSPInventories[i].TenantID] = append(cfg.dspMap[cfg.DSPInventories[i].TenantID], cfg.DSPInventories[i])
		}
	}

	m.config.Store(&cfg)
	return nil
}

func (m *Manager) IsHealthy() bool {
	cfg := m.config.Load()
	if cfg == nil {
		return false
	}
	// Double check freshness just in case reloader failed but old config was kept
	return (time.Now().Unix() - cfg.TS) <= 600
}

func (m *Manager) StartReloading(ctx context.Context, path string) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := m.Load(path); err != nil {
					logger.Errorf("Failed to reload partners config: %v", err)
				} else {
					logger.Infof("Successfully reloaded partners config from %s", path)
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *Manager) GetConfig() *PartnersConfig {
	return m.config.Load()
}

func (m *Manager) GetSSPByInventoryCode(code string) (*SSPInventory, bool) {
	cfg := m.GetConfig()
	if cfg == nil || cfg.sspMap == nil {
		return nil, false
	}
	ssp, ok := cfg.sspMap[code]
	return ssp, ok
}

func (m *Manager) GetDSPsByTenant(tenantID int) []DSPInventory {
	cfg := m.GetConfig()
	if cfg == nil || cfg.dspMap == nil {
		return nil
	}
	return cfg.dspMap[tenantID]
}
