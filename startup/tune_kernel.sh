#!/bin/bash

# Prebid Server Cluster - Host Kernel Tuning Script
# Optimization for High-Concurrency RTB Environments

if [[ $EUID -ne 0 ]]; then
   echo "This script must be run as root (or via sudo)"
   exit 1
fi

echo "--- Optimizing Linux Kernel for Prebid Server Cluster ---"

# 1. Increase max backlog for incoming connections
sysctl -w net.core.somaxconn=10000
sysctl -w net.ipv4.tcp_max_syn_backlog=10000

# 2. Enable port reuse for faster connection cycling (Critical for high DSP concurrency)
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w net.ipv4.tcp_fin_timeout=15

# 3. Expand ephemeral port range for massive parallel DSP calls
sysctl -w net.ipv4.ip_local_port_range="10000 65535"

# 4. Persistence - Create sysctl.d config
CONF_FILE="/etc/sysctl.d/99-prebid-server.conf"

echo "# Prebid Server RTB Optimizations" > $CONF_FILE
echo "net.core.somaxconn = 10000" >> $CONF_FILE
echo "net.ipv4.tcp_max_syn_backlog = 10000" >> $CONF_FILE
echo "net.ipv4.tcp_tw_reuse = 1" >> $CONF_FILE
echo "net.ipv4.tcp_fin_timeout = 15" >> $CONF_FILE
echo "net.ipv4.ip_local_port_range = 10000 65535" >> $CONF_FILE

echo "--- Kernel Optimized Successfully! Settings persisted to $CONF_FILE ---"
