# Prebid Server Startup & Deployment Guide

This directory contains configuration files for running multiple high-performance instances of **Prebid Server** on a single VM. It uses a **30-port isolation strategy** to prevent service collisions and integrates with Prometheus/Grafana for full observability.

## 1. Port Allocation Map (Batch 30)

Each instance occupies a "lane" of 30 ports starting from `24000`.

| Instance | Admin/Health | RTB Ports (Main Traffic) | Prometheus Metrics |
| :--- | :--- | :--- | :--- |
| **Inst 1** | `24000` | `24001 - 24020` | `24029` |
| **Inst 2** | `24030` | `24031 - 24050` | `24059` |
| **Inst 3** | `24060` | `24061 - 24080` | `24089` |
| **Inst 4** | `24090` | `24091 - 24110` | `24119` |
| **Inst 5** | `24120` | `24121 - 24140` | `24149` |
| **Vector Bids** | - | - | `49598` |
| **Vector Win** | - | - | `49599` |
| **Vector Track** | - | - | `49600` |

---

## 2. Option A: PM2 Deployment (Recommended)

PM2 is recommended for managing both Prebid Server instances and Mock Simulators. It provides automatic restarts and easy log management.

### Start Prebid Server Cluster

```bash
# Start all 5 instances using the absolute path to the binary
pm2 start startup/ecosystem.config.js
```

### Start Mock Simulators (Traffic Generation)

```bash
# Start SSP, DSP, Win Gen, and Receiver simulators
# (Note: simulators.config.js is located in the simulators folder)
pm2 start /opt/adserving/14-feb-2026/cd_adhoc/mock_simulators/simulators.config.js
```

### Management Commands

```bash
# View all running processes
pm2 list
```

```bash
# Save current process list to auto-start on boot (CRITICAL)
pm2 save
```

```bash
# View real-time logs
pm2 logs prebid-server-1
pm2 logs mock-ssp

# Monitor resource usage (CPU/Mem)
pm2 monit
```

---

## 3. Option B: Systemd Deployment (Core Services)

These infrastructure components should be managed by systemd to ensure they are always running and survive reboots.

### 3.1. Infrastructure Installation (If Binaries are Missing)

If official binaries for **Pushgateway** or **Node Exporter** are not found in `/usr/local/bin/`, install them as follows:

**Installing Pushgateway:**
```bash
wget https://github.com/prometheus/pushgateway/releases/download/v1.10.0/pushgateway-1.10.0.linux-amd64.tar.gz
tar -xf pushgateway-1.10.0.linux-amd64.tar.gz
sudo mv pushgateway-1.10.0.linux-amd64/pushgateway /usr/local/bin/
sudo chmod +x /usr/local/bin/pushgateway
rm -rf pushgateway-1.10.0.linux-amd64*
```

**Installing Node Exporter:**
```bash
wget https://github.com/prometheus/node_exporter/releases/download/v1.8.2/node_exporter-1.8.2.linux-amd64.tar.gz
tar -xf node_exporter-1.8.2.linux-amd64.tar.gz
sudo mv node_exporter-1.8.2.linux-amd64/node_exporter /usr/local/bin/
sudo chmod +x /usr/local/bin/node_exporter
rm -rf node_exporter-1.8.2.linux-amd64*
```

### 3.2. Setup Instructions (Relinking & Activation)
Inside the `startup/` folder, you will find systemd unit files. To activate them:

```bash
# 1. Link & Enable Pushgateway
sudo ln -sf $(pwd)/pushgateway.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pushgateway

# 2. Link & Enable Node Exporter
sudo ln -sf $(pwd)/node_exporter.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now node_exporter

# 3. Link & Enable Prometheus
sudo ln -sf $(pwd)/prometheus.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now prometheus
```

---

## 4. Verification & Health Checks

Verify a specific instance is responding correctly:

```bash
# Check Instance 1 Health
curl http://localhost:24000/status

# Check Instance 2 Health
curl http://localhost:24030/status
```

---

## 5. Load Balancer Configuration

Your upstream load balancer (e.g., HAProxy or Nginx) should be configured to distribute traffic across the **RTB Port Ranges** for maximum parallel throughput.

---

## 6. Monitoring & Dashboards (Prometheus/Grafana)

The monitoring configuration is located in `startup/prometheus/` and `startup/grafana/`.

### 6.2. Multi-Host Centralized Scrape Configuration (Recommended)

In a 3-node (`ad-1`, `ad-2`, `ad-3`) + 1-monitor (`monit-1`) setup, use **File-Based Discovery** to keep `prometheus.yml` ultra-simple and manage targets in separate files.

**Master Configuration (`/etc/prometheus/prometheus.yml` on monit-1):**
```yaml
scrape_configs:
  - job_name: 'prebid_cluster'
    file_sd_configs:
      - files: ['/etc/prometheus/rules/prebid_targets.yml']

  - job_name: 'node'
    file_sd_configs:
      - files: ['/etc/prometheus/rules/node_targets.yml']
```

**Discovery File Setup (`/etc/prometheus/rules/prebid_targets.yml`):**
List your machines and assign a `device_name` label to each. This enables filtering in Grafana:
```yaml
- targets: ['ad-1:24029', 'ad-1:24059', ...]
  labels:
    device_name: 'ad-1'
- targets: ['ad-2:24029', 'ad-2:24059', ...]
  labels:
    device_name: 'ad-2'
```

### 6.3. Pushgateway Metric Flow (Batch Tracking)

In a cluster, individual machines (`ad-X`) push their ephemeral metrics (like sync results or script status) to the central `monit-1` machine.

1.  **On ad-1/2/3**: Your scripts/apps push metrics to the central Pushgateway:
    `echo "job_success 1" | curl --data-binary @- http://monit-1:9091/metrics/job/my_script_name`
2.  **On monit-1**: Prometheus scrapes its local port `9091` and reveals all the pushed metrics from all machines at once.
3.  **Grafana**: You can then visualization "Device-Level Sync Status" across the whole fleet.

### 6.4. Vector Monitoring (Pull-Based Scraper)

Unlike batch scripts, Vector collectors expose a persistent scraper port. This ensures high-throughput monitoring without the overhead of thousands of HTTP pushes.

1.  **Default Ports**: Bids (`49598`), Win (`49599`), Track (`49600`).
2.  **Safe Start (Port Conflict Override)**:
    If a port is already in use, you can start Vector with a random dynamic port to ensure production log collection is not interrupted:
    ```bash
    VECTOR_WIN_PORT="0.0.0.0:0" vector --config <path_to_yaml>
    ```
3.  **Monit-1 Scraper**: Prometheus on `monit-1` is configured to pull from these ports on all ad-nodes using `startup/prometheus/vector_targets.yml`.

### Grafana Dashboards

Import the provided JSON files in `startup/grafana/` into your Grafana instance:

- **pbs_cluster_summary.json**: High-level overview of the entire VM cluster.
- **rtb_global_summary.json**: Detailed breakdown of global traffic.
- **rtb_ssp_performance.json**: Latency and success rates per SSP.
- **rtb_dsp_performance.json**: Timeouts and bid rates per DSP.

---

## 7. Production Kernel Tuning (Sysctl)

To handle 100k+ QPS and avoid port exhaustion:

```bash
sudo ./startup/tune_kernel.sh
```

---

## 8. High-Speed URL Reduction & Tracking (Production V7)

To support 100k+ QPS on lean infrastructure and keep URL lengths minimal for SSP compatibility, we use an optimized **Multi-Algorithm Compression** strategy:

### Optimized Parameter Strategy

- **`d` Parameter (DSP URL)**: Compressed with **Brotli** (Quality 4).
- **`x` Parameter (Auction Payload)**: Compressed with **Zlib** (BestCompression).

### Tracking Payload (`x` parameter) Structure

The `x` parameter contains a binary buffer packed in the following **Positional Order** (Big-Endian):

1. **Block 1 (Fixed-Width 32 Bytes)**:
    - `Timestamp` (4B), `TenantID` (4B), `SSPID` (4B), `SSPInvID` (4B), `DSPID` (4B), `DSPInvID` (4B), `Price` (4B fixed-point), `DeviceType` (1B), `OS` (1B), `AdType` (1B)
2. **Block 2 (UUIDs 48 Bytes)**:
    - `AuctionID` (16B), `BidID` (16B), `ImpID` (16B)
3. **Block 3 (Variable Strings - Length Prefixed)**:
    - `OSV`, `Country`, `AdSize`, `Domain`, `BundleID`, `Carrier`, `Seat`, `AdID`
