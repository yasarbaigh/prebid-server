package partners

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prebid/prebid-server/v3/logger"
)

const (
	DefaultMargin      = 20.0
	DefaultMinBidFloor = 0.05
	DefaultMaxBidFloor = 100.0
	DefaultTimeout     = 120
	DefaultTMax        = 120
	DefaultQPSLimit    = 50
	DefaultPricingAt   = 1
)

// FlexFloat64 handles both string and numeric float64 values in JSON
type FlexFloat64 float64

func (f *FlexFloat64) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*f = 0
			return nil
		}
		var val float64
		if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
			*f = 0
			return nil
		}
		*f = FlexFloat64(val)
		return nil
	}
	var val float64
	if err := json.Unmarshal(b, &val); err != nil {
		return err
	}
	*f = FlexFloat64(val)
	return nil
}

// FlexInt handles both string and numeric integer values in JSON
type FlexInt int

func (i *FlexInt) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*i = 0
			return nil
		}
		var val int
		if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
			*i = 0
			return nil
		}
		*i = FlexInt(val)
		return nil
	}
	var val int
	if err := json.Unmarshal(b, &val); err != nil {
		return err
	}
	*i = FlexInt(val)
	return nil
}

type SSPInventory struct {
	Name                 string      `json:"name"`
	ID                   FlexInt     `json:"id"`
	InventoryName        string      `json:"inventory_name"`
	Status               string      `json:"status"`
	InventoryCode        string      `json:"inventory_code"`
	TenantIdentifier     string      `json:"tenant_identifier"`
	SSPIdentifier        string      `json:"ssp_identifier"`
	TenantID             FlexInt     `json:"tenant_id"`
	SSPID                FlexInt     `json:"ssp_id"`
	SSPInventoryID       FlexInt     `json:"ssp_inventory_id"`
	SSPInventoryIdentifier string     `json:"ssp_inventory_identifier"`
	AdFormats            []string    `json:"ad_formats"`
	WinURL               string      `json:"win_url"`
	ImpTrackURL          string      `json:"imp_track_url"`
	ClickTrackURL        string      `json:"click_track_url"`
	WinBaseDmn           string      `json:"win_base_dmn"`
	TrackBaseDmn         string      `json:"track_base_dmn"`
	AdmPriceTransparency bool        `json:"adm_price_transparency"`
	SChainNode           string      `json:"schain_node"` // Your exchange identity for this SSP
	Timeout              FlexInt     `json:"timeout"`
	FloorPrice           FlexFloat64 `json:"floor_price"`
	RevenueShare         FlexFloat64 `json:"revenue_share"`
	FixedCPM             FlexFloat64 `json:"fixed_cpm"`
}

type DSPInventory struct {
	Name                 string      `json:"name"`
	DSPIdentifier        string      `json:"dsp_identifier"`
	EndpointName         string      `json:"endpoint_name"`
	EndpointURL          string      `json:"endpoint_url"`
	QPS                  FlexInt     `json:"qps_limit"`
	Tmax                 FlexInt     `json:"tmax"`
	ID                   FlexInt     `json:"id"`
	InventoryCode        string      `json:"inventory_code"`
	Status               string      `json:"status"`
	MinBidFloor          FlexFloat64 `json:"min_bidfloor"`
	MaxBidFloor          FlexFloat64 `json:"max_bidfloor"`
	AdFormats            []string    `json:"ad_formats"`
	Source               []string    `json:"source"`
	Country              []string    `json:"country"`
	CountryBlackList     []string    `json:"country_black_list"`
	IABCategories        []string    `json:"iab_categories"`
	BundleIDs            []string    `json:"bundle_ids"`
	BundleIDsBlackList   []string    `json:"bundle_ids_black_list"`
	SSPs                 []string    `json:"ssps"`
	SSPsBlackList        []string    `json:"ssps_black_list"`
	Publishers           []string    `json:"publishers"`
	PublishersBlackList  []string    `json:"publishers_black_list"`
	TenantIdentifier     string      `json:"tenant_identifier"`
	TenantID             FlexInt     `json:"tenant_id"`
	DSPID                FlexInt     `json:"dsp_id"`
	DSPInventoryID       FlexInt     `json:"dsp_inventory_id"`
	DSPInventoryIdentifier string       `json:"dsp_inventory_identifier"`
	AdmPriceTransparency   bool         `json:"adm_price_transparency"`
	Margin                 FlexFloat64  `json:"margin"`
	BidAdjustment          float64      `json:"bid_adjustment"` // Multiplier (e.g. 0.9)
	PricingAt            FlexInt         `json:"pricing_at"`
	Timeout              FlexInt         `json:"timeout"`
	AdFormatsMap         map[string]bool `json:"-"`
	CountryMap           map[string]bool `json:"-"`
	CountryBlackListMap  map[string]bool `json:"-"`
	BundleIDsMap         map[string]bool `json:"-"`
	BundleIDsBlackListMap map[string]bool `json:"-"`
	SSPsMap              map[string]bool `json:"-"`
	SSPsBlackListMap     map[string]bool `json:"-"`
	SourceMap            map[string]bool `json:"-"`
}

type PartnersConfig struct {
	SSPInventories []SSPInventory `json:"ssp_inventories"`
	DSPInventories []DSPInventory `json:"dsp_inventories"`
	AdServing      bool           `json:"ad_serving"`
	ASI            string         `json:"asi"`
	TS             int64          `json:"-"` // Internal Unix timestamp

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

	// Helper struct to handle string TS from JSON
	var raw struct {
		SSPInventories []SSPInventory `json:"ssp_inventories"`
		DSPInventories []DSPInventory `json:"dsp_inventories"`
		AdServing      bool           `json:"ad_serving"`
		ASI            string         `json:"asi"`
		TS             string         `json:"ts"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		m.config.Store(nil)
		return fmt.Errorf("failed to unmarshal partners config: %v", err)
	}

	// Parse Datetime String: 2026-03-18 11:11:11
	parsedTime, err := time.Parse("2006-01-02 15:04:05", raw.TS)
	if err != nil {
		// Fallback for debugging if it's still a number
		m.config.Store(nil)
		return fmt.Errorf("invalid TS format in partners.json: %s (expected YYYY-MM-DD HH:MM:SS)", raw.TS)
	}

	cfg := PartnersConfig{
		SSPInventories: raw.SSPInventories,
		DSPInventories: raw.DSPInventories,
		AdServing:      raw.AdServing,
		ASI:            raw.ASI,
		TS:             parsedTime.Unix(),
	}

	// Strict TS Check: Stop serving if TS is older than 10 minutes
	now := time.Now().Unix()
	if cfg.TS == 0 || (now-cfg.TS) > 600 {
		m.config.Store(nil)
		return fmt.Errorf("partners config TS is stale or missing (TS: %d, Now: %d, Raw: %s)", cfg.TS, now, raw.TS)
	}

	// Default Values and Robustness checks
	for i := range cfg.SSPInventories {
		if cfg.SSPInventories[i].Timeout <= 0 {
			cfg.SSPInventories[i].Timeout = FlexInt(DefaultTimeout)
		}
	}

	for i := range cfg.DSPInventories {
		if cfg.DSPInventories[i].Tmax <= 0 {
			cfg.DSPInventories[i].Tmax = FlexInt(DefaultTMax)
		}
		if cfg.DSPInventories[i].Timeout <= 0 {
			cfg.DSPInventories[i].Timeout = FlexInt(DefaultTimeout)
		}
		if cfg.DSPInventories[i].QPS <= 0 {
			cfg.DSPInventories[i].QPS = FlexInt(DefaultQPSLimit)
		}
		if cfg.DSPInventories[i].PricingAt == 0 {
			cfg.DSPInventories[i].PricingAt = FlexInt(DefaultPricingAt)
		}
		if cfg.DSPInventories[i].Margin <= 0 || cfg.DSPInventories[i].Margin > 100.0 {
			cfg.DSPInventories[i].Margin = FlexFloat64(DefaultMargin)
		}
		if cfg.DSPInventories[i].MinBidFloor <= 0 {
			cfg.DSPInventories[i].MinBidFloor = FlexFloat64(DefaultMinBidFloor)
		}
		if cfg.DSPInventories[i].MaxBidFloor <= 0 {
			cfg.DSPInventories[i].MaxBidFloor = FlexFloat64(DefaultMaxBidFloor)
		}
	}

	// Build Lookup Maps for O(1) performance
	cfg.sspMap = make(map[string]*SSPInventory)
	for i := range cfg.SSPInventories {
		cfg.sspMap[cfg.SSPInventories[i].InventoryCode] = &cfg.SSPInventories[i]
	}

	cfg.dspMap = make(map[int][]DSPInventory)
	for i := range cfg.DSPInventories {
		dsp := &cfg.DSPInventories[i]
		// Pre-populate maps for O(1) matching
		dsp.AdFormatsMap = listToMap(dsp.AdFormats, true)
		dsp.CountryMap = listToMap(dsp.Country, true)
		dsp.CountryBlackListMap = listToMap(dsp.CountryBlackList, true)
		dsp.BundleIDsMap = listToMap(dsp.BundleIDs, true)
		dsp.BundleIDsBlackListMap = listToMap(dsp.BundleIDsBlackList, true)
		dsp.SSPsMap = listToMap(dsp.SSPs, true)
		dsp.SSPsBlackListMap = listToMap(dsp.SSPsBlackList, true)
		dsp.SourceMap = listToMap(dsp.Source, true)

		// Only index Active DSPs
		if dsp.Status == "Active" {
			cfg.dspMap[int(dsp.TenantID)] = append(cfg.dspMap[int(dsp.TenantID)], *dsp)
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
func listToMap(list []string, lowercase bool) map[string]bool {
	m := make(map[string]bool)
	for _, s := range list {
		if lowercase {
			m[strings.ToLower(s)] = true
		} else {
			m[s] = true
		}
	}
	return m
}
