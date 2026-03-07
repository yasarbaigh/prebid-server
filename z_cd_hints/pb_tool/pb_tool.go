package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/prebid/prebid-server/v3/proto/generated"
	"google.golang.org/protobuf/proto"
)

// go run z_cd_hints/pb_tool/pb_tool.go decode -hex <PASTE_HEX_STRING_HERE>

func main() {
	decodeCmd := flag.NewFlagSet("decode", flag.ExitOnError)
	hexFlag := decodeCmd.String("hex", "", "Hex string to decode")

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run z_cd_hints/pb_tool.go decode -hex <hex_string>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "decode":
		decodeCmd.Parse(os.Args[2:])
		if *hexFlag == "" {
			fmt.Println("Please provide a hex string using -hex flag")
			os.Exit(1)
		}

		data, err := hex.DecodeString(*hexFlag)
		if err != nil {
			fmt.Printf("Failed to decode hex: %v\n", err)
			os.Exit(1)
		}

		event := &generated.AuctionEvent{}
		if err := proto.Unmarshal(data, event); err != nil {
			fmt.Printf("Failed to unmarshal protobuf: %v\n", err)
			fmt.Println("Hint: Ensure you copied the FULL hex line from the log file.")
			os.Exit(1)
		}

		// Create a map to customize JSON output for raw bytes
		output := map[string]interface{}{
			"tenant_id":              event.TenantId,
			"ssp_partner_id":         event.SspPartnerId,
			"ssp_inventory_id":       event.SspInventoryId,
			"ssp_partner_auction_id": event.SspPartnerAuctionId,
			"dsp_partner_id":         event.DspPartnerId,
			"dsp_inventory_id":       event.DspInventoryId,
			"bid_request_price":      event.BidRequestPrice,
			"dsp_price":              event.DspPrice,
			"timestamp":              event.Timestamp,
			"hostname":               event.Hostname,
			"raw_bid_request_json":   json.RawMessage(event.RawBidRequest),
			"raw_dsp_response_json":  json.RawMessage(event.RawDspResponse),
		}

		// Handle OneOf Source field (App/Web)
		if event.GetApp() != nil {
			output["source_app"] = event.GetApp()
		} else if event.GetWeb() != nil {
			output["source_web"] = event.GetWeb()
		}

		jsonBytes, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			fmt.Printf("Failed to marshal to JSON: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonBytes))

	default:
		fmt.Println("Unknown command. Use 'decode'.")
		os.Exit(1)
	}
}
