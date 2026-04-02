const fs = require('fs');
const { execSync } = require('child_process');

// Centralized Environment Loading
const envPath = '/opt/app_adserving/fixed/ssp_app.env';
let extraEnv = {};
if (fs.existsSync(envPath)) {
  const content = fs.readFileSync(envPath, 'utf8');
  content.split('\n').forEach(line => {
    const trimmedLine = line.trim();
    if (trimmedLine && !trimmedLine.startsWith('#')) {
      const parts = trimmedLine.split('=');
      if (parts.length >= 2) {
        extraEnv[parts[0].trim()] = parts.slice(1).join('=').trim();
      }
    }
  });
}

const basePort = 24000;
const portInterval = 30;
const numInstances = 5;


const apps = [];

// 1. Check for Build Flag (Run: BUILD_PREBID=true pm2 start ecosystem.config.js)
if (process.env.BUILD_PREBID === 'true') {
  console.log("Starting Prebid Server Build...");
  try {
    // Builds the binary in the root directory
    execSync('go build -v -o prebid-server.bin .', { stdio: 'inherit' });
    console.log("Build Successful.");
  } catch (err) {
    console.error("Build Failed. Aborting startup.");
    process.exit(1);
  }
}

for (let i = 1; i <= numInstances; i++) {
  const start = basePort + (i - 1) * portInterval;
  const healthPort = start;
  const rtbPorts = [];
  for (let j = 1; j <= 20; j++) {
    rtbPorts.push(start + j);
  }
  const prometheusPort = start + 29;

  apps.push({
    name: `prebid-server-${i}`,
    script: "./prebid-server.bin",
    args: "",
    env: {
      ...extraEnv,
      PBS_ADMIN_PORT: healthPort,
      PBS_PORTS: rtbPorts.join(","),
      PBS_METRICS_PROMETHEUS_PORT: prometheusPort,
      // Default to 8000 for standard pbs.port if not using multiple ports
      PBS_PORT: rtbPorts[0],
      INSTANCE_ID: i,
      GOMEMLIMIT: "2GiB", // Explicit memory protection
      GOGC: "200"       // Optimized GC cycle for RTB
    },
    // Log files per instance
    error_file: `/opt/service_logs/ssp_module/ssp_bids/pbs_${i}_err.log`,
    out_file: `/opt/service_logs/ssp_module/ssp_bids/pbs_${i}_out.log`,
    log_date_format: "YYYY-MM-DD HH:mm:ss Z",
    autorestart: true,
    watch: false,
    max_memory_restart: "1G"
  });
}

module.exports = {
  apps: apps
};
