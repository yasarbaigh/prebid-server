# Prometheus and Grafana Monitoring Guide - Prebid Server

This guide explains how to set up, use, and maintain the Prometheus-based monitoring system for your Prebid Server cluster. It is optimized for multi-tenant and multi-partner tracking using the `V7` high-performance metric identifiers.

---

## 1. Metrics & Labeling Configuration

All Prebid Server custom metrics are prefixed with the namespace `prebid_` to avoid collisions. The system uses a specific set of 5 labels for deep analytics:

*   **`tenant_identifier`**: Links auction traffic to a specific customer.
*   **`ssp_identifier`**: Identifies the SSP/Publisher source.
*   **`ssp_inventory_prometheus_identifier`**: Granular tracking for a specific SSP inventory/site.
*   **`dsp_identifier`**: Identifies the DSP/Bidder.
*   **`dsp_inventory_prometheus_identifier`**: Granular tracking for a specific DSP seat/deal.

---

## 2. Prometheus Setup (`prometheus/`)

The core configuration is located in the `prometheus/` subdirectory.

### Scrape Configuration
The main `prometheus.yml` is configured to scrape all 5 local instances of Prebid Server:
*   Instances: `localhost:24029`, `localhost:24059`, etc.
*   **Relabeling**: It automatically maps addresses to human-readable names like `pbs_24029` for easier dashboard filtering.

### Recording & Alerting Rules
The system includes two pre-optimized rule files that significantly improve dashboard performance:

*   **`ssp_rules.yml`**: Pre-calculates SSP request/success/no-bid rates. Includes a **critical alert** if the SSP error rate exceeds 10%.
*   **`dsp_rules.yml`**: Pre-calculates DSP fan-out rates, P95 latency distributions, and bid ratios. Includes a **warning alert** if DSP latency exceeds 300ms or errors exceed 20%.

**How to load rules:**
1. Ensure they are listed in the `rule_files` section of `prometheus.yml`.
2. Reload by running: `curl -X POST http://localhost:9090/-/reload`

---

## 3. Grafana Dashboards (`grafana/`)

We have provided a comprehensive dashboard for deep analysis of auction performance.

### `prebid_server_dashboard.json`
*   **Total Auctions**: Live throughput with success/rejection status.
*   **Partner Comparison**: Stacked views of SSP request volume vs. DSP participation (bid-rate).
*   **Latency Heatmap**: Monitors how each DSP is performing against your TMax.
*   **Economics**: Tracks Net Exchange Profit, Revenue, and Spent in real-time.

### How to Import
1. In Grafana, click **+ > Import**.
2. Upload the `grafana/prebid_server_dashboard.json` file.
3. Select your Prometheus datasource and click **Import**.

---

## 4. Operational Commands for Reliability

Check if rules are active:
`http://localhost:9090/rules`

Check internal scraping health:
`http://localhost:9090/targets`

Verify recorded metric activity (fast):
`sum(job:dsp_requests:rate5m) by (dsp_identifier)`

---

*Last Updated: 2026-03-19*
