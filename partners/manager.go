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
}

type PartnersConfig struct {
	SSPInventories []SSPInventory `json:"ssp_inventories"`
	DSPInventories []DSPInventory `json:"dsp_inventories"`
	AdServing      bool           `json:"ad_serving"`
	TS             string         `json:"ts"`
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
		return fmt.Errorf("failed to read partners file: %v", err)
	}

	var cfg PartnersConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal partners config: %v", err)
	}

	m.config.Store(&cfg)
	return nil
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
	if cfg == nil {
		return nil, false
	}
	for i := range cfg.SSPInventories {
		if cfg.SSPInventories[i].InventoryCode == code {
			return &cfg.SSPInventories[i], true
		}
	}
	return nil, false
}

func (m *Manager) GetDSPsByTenant(tenantID int) []DSPInventory {
	cfg := m.GetConfig()
	if cfg == nil {
		return nil
	}
	var dsps []DSPInventory
	for i := range cfg.DSPInventories {
		if cfg.DSPInventories[i].TenantID == tenantID && cfg.DSPInventories[i].Status == "Active" {
			dsps = append(dsps, cfg.DSPInventories[i])
		}
	}
	return dsps
}
