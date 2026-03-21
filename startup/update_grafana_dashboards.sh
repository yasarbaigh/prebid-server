#!/bin/bash

# Configuration
GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASS="admin"
DASHBOARD_DIR="./grafana"

echo "Updating Grafana dashboards from $DASHBOARD_DIR..."

for file in "$DASHBOARD_DIR"/*.json; do
    if [ -f "$file" ]; then
        echo "Processing $file..."
        
        # Wrap the dashboard JSON in the format expected by the API
        dashboard_json=$(cat "$file")
        payload=$(jq -n --argjson db "$dashboard_json" '{dashboard: $db, overwrite: true, folderId: 0}')
        
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
