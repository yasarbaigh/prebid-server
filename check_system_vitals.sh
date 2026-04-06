#!/bin/bash
# Prebid Server System Vitals Check
# This script contains a list of commands to check system load, disk usage, and other main vitals.

set -e

echo "=========================================================="
echo "          SYSTEM VITALS & PERFORMANCE REPORT              "
echo "=========================================================="
date

echo -e "\n\e[1;34m[1] SYSTEM LOAD & UPTIME\e[0m"
uptime
echo "CPU Cores: $(nproc)"
vmstat 1 2 | tail -n 1 | awk '{print "Context Switches: " $12 " | Interrupts: " $11}'

echo -e "\n\e[1;34m[2] CPU & I/O STATISTICS\e[0m"
iostat -c | head -n 4 | tail -n 2

echo -e "\n\e[1;34m[3] MEMORY USAGE\e[0m"
free -h

echo -e "\n\e[1;34m[4] DISK USAGE & HEALTH\e[0m"
# Root filesystem check
df -h / | awk 'NR==2 {print "Root Drive Usage: " $3 " / " $2 " (" $5 ")"}'

# Workspace and log directories
echo -e "\nWorkspace and Log sizes:"
du -sh /opt/service_logs/ssp_module/ssp_bids/ | awk '{print "  Prebid Logs Directory  : ", $1}'

echo -e "\n\e[1;34m[5] PM2 PROCESS STATUS\e[0m"
pm2 status

echo -e "\n\e[1;34m[6] NETWORK LISTENING PORTS (PBS)\e[0m"
# Checking specifically for PBS related ports (24000+ as per ecosystem.config.js)
ss -tulpn | grep 240[0-9]{2} | head -n 10

echo -e "\n\e[1;34m[7] LATEST PBS LOG ERRORS (LAST 20 LINES)\e[0m"
# Checking for errors in recently updated pbs instance logs
tail -n 20 /opt/service_logs/ssp_module/ssp_bids/pbs_*_err.log 2>/dev/null | grep -iE "error|fail|critical|fatal" | head -n 20 || echo "  No critical errors found in recent log snapshots."

echo -e "\n=========================================================="
echo "          END OF VITALS STATUS REPORT                     "
echo "=========================================================="
