# Prebid Server Startup & Deployment Guide

This directory contains configuration files for running multiple high-performance instances of **Prebid Server** on a single VM. It uses a **30-port isolation strategy** to prevent service collisions.

## 1. Port Allocation Map (Batch 30)

Each instance occupies a "lane" of 30 ports starting from `24000`.

| Instance | Admin/Health | RTB Ports (Main Traffic) | Prometheus Metrics |
| :--- | :--- | :--- | :--- |
| **Inst 1** | `24000` | `24001 - 24020` | `24029` |
| **Inst 2** | `24030` | `24031 - 24050` | `24059` |
| **Inst 3** | `24060` | `24061 - 24080` | `24089` |
| **Inst 4** | `24090` | `24091 - 24110` | `24119` |
| **Inst 5** | `24120` | `24121 - 24140` | `24149` |

---

## 2. Option A: PM2 Deployment (Recommended for Local Dev/Debug)

PM2 allows for easy log viewing and process management.

### Commands

```bash
# Start all 5 instances
pm2 start startup/ecosystem.config.js

# View real-time logs
pm2 logs prebid-server-1

# Monitor resource usage (CPU/Mem)
pm2 monit

# Stop or restart instances
pm2 restart all
pm2 delete all
```

---

## 3. Option B: Systemd Deployment (Production)

Recommended for production as it handles system reboots and provides native OS isolation.

### One-Time Setup

```bash
# Link the service template
cp /opt/adserving/14-feb-2026/1_prebid-server/startup/prebid-server@.service /etc/systemd/system/
systemctl daemon-reload
```

### Start Individual Instances

```bash
# Start Instance 1
systemctl start prebid-server@1

# Enable auto-start on boot
systemctl enable prebid-server@1 prebid-server@2 prebid-server@3
```

### View Status

```bash
systemctl status prebid-server@1
journalctl -u prebid-server@1 -f
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

Your upstream load balancer (e.g., Nginx or HAProxy) should be configured to distribute traffic across the **RTB Port Ranges** for maximum parallel throughput.

### Example for Instance 1

- **Target IP**: `127.0.0.1`
- **Target Ports**: `24001, 24002, 24003 ... 24020`

---

## 6. Monitoring & Dashboards (Prometheus/Grafana)

The monitoring configuration is located in `startup/prometheus/` and `startup/grafana/`.

### Prometheus Integration

To collect metrics from all 5 instances:

1. Copy or include `startup/prometheus/prometheus.yml` in your main Prometheus config.
2. The targets are pre-configured to hit the Prometheus ports (e.g., `24029`, `24059`).
3. Load the custom rules from `startup/prometheus/dsp_rules.yml` and `ssp_rules.yml` for calculating derived rates and alerts.

### Grafana Dashboards

Import the provided JSON files in `startup/grafana/` into your Grafana instance:

- **pbs_cluster_summary.json**: High-level overview of the entire VM cluster.
- **rtb_global_summary.json**: Detailed breakdown of global traffic.
- **rtb_ssp_performance.json**: Latency and success rates per SSP.
- **rtb_dsp_performance.json**: Timeouts and bid rates per DSP.

---

## 7. Production Kernel Tuning (Sysctl)

To handle 100k+ QPS and avoid port exhaustion, you must optimize the host's TCP stack. We have provided a helper script for this.

### Auto-Tuning (Recommended)

Run the following inside the `startup/` directory of the new machine:

```bash
sudo ./startup/tune_kernel.sh
```

This script will apply the settings immediately and persist them to `/etc/sysctl.d/99-prebid-server.conf` so they survive system reboots.

### Manual Tuning

If you prefer to manually edit your configuration:

```bash
# Add to /etc/sysctl.conf or sysctl.d/
net.core.somaxconn = 10000
net.ipv4.tcp_max_syn_backlog = 10000
net.ipv4.ip_local_port_range = 10000 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
```

Apply manually with `sysctl -p`.
