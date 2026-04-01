#!/bin/bash

# Configuration
GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASS="admin"
#GRAFANA_PASS="London@123"

DASHBOARD_DIR="./grafana"

echo "Updating Grafana dashboards from $DASHBOARD_DIR..."

for file in "$DASHBOARD_DIR"/*.json; do
    if [ -f "$file" ]; then
        echo "Processing $file..."
        
        # 1. READ and SANITIZE the dashboard JSON
        # Replaces hardcoded DS_PROMETHEUS references with a universal variable
        dashboard_raw=$(cat "$file" | \
            sed 's/DS_PROMETHEUS[A-Z0-9_]*/datasource/g' | \
            sed 's/"datasource": "Prometheus"/"datasource": "${datasource}"/g')
        
        # 2. ENFORCE templating variable if missing (inject using jq)
        final_json=$(echo "$dashboard_raw" | jq '
            if .templating.list | map(.name == "datasource") | any | not then
                .templating.list = [{
                    "name": "datasource",
                    "query": "prometheus",
                    "type": "datasource"
                }] + .templating.list
            else . end')

        # 3. Wrap for API
        payload=$(jq -n --argjson db "$final_json" '{dashboard: $db, overwrite: true, folderId: 0}')
        
        # Send to Grafana API
        response=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASS" \
            -H "Content-Type: application/json" \
            -X POST \
            -d "$payload" \
            "$GRAFANA_URL/api/dashboards/db")
        
        # Check for success
        if echo "$response" | grep -q '"status":"success"'; then
            echo "Successfully updated: $(basename "$file")"
        else
            echo "FAILED to update: $(basename "$file")"
            echo "Response: $response"
        fi
    fi
done

echo "Done."
