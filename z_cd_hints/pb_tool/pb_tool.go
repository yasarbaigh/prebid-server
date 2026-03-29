package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/prebid/prebid-server/v3/proto/generated"
	"google.golang.org/protobuf/proto"
)

func main() {
	fileFlag := flag.String("file", "", "File path containing list of base64 auction proto strings")
	b64Flag := flag.String("b64", "", "Single base64 string to decode")
	flag.Parse()

	if *fileFlag == "" && *b64Flag == "" {
		fmt.Println("Usage:")
		fmt.Println("  go run z_cd_hints/pb_tool/pb_tool.go -file /tmp/dummy/auction_proto_text.txt")
		fmt.Println("  go run z_cd_hints/pb_tool/pb_tool.go -b64 <base64_string>")
		os.Exit(1)
	}

	if *fileFlag != "" {
		processFile(*fileFlag)
	} else if *b64Flag != "" {
		decodeAndPrint(*b64Flag)
	}
}

func processFile(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Use a large buffer for long proto lines
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fmt.Printf("\n--- [Line %d] ---\n", lineNum)
		decodeAndPrint(line)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading file: %v\n", err)
	}
}

func decodeAndPrint(b64Str string) {
	// Robust cleaning: only keep valid Base64 characters
	var cleaner strings.Builder
	for _, r := range b64Str {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '-' || r == '_' || r == '=' {
			cleaner.WriteRune(r)
		}
	}
	cleanStr := cleaner.String()

	data, err := base64.StdEncoding.DecodeString(cleanStr)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(cleanStr)
	}
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(cleanStr)
	}
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(cleanStr)
	}

	if err != nil {
		fmt.Printf("Error decoding base64: %v\n", err)
		return
	}

	event := &generated.AuctionEvent{}
	if err := proto.Unmarshal(data, event); err != nil {
		fmt.Printf("Error unmarshaling protobuf: %v\n", err)
		return
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
		"ssp_price":              event.SspPrice,
		"timestamp":              event.Timestamp,
		"hostname":               event.Hostname,
		"device_type":            event.DeviceType,
		"os":                     event.Os,
		"osv":                    event.Osv,
		"country":                event.Country,
		"ad_type":                event.AdType,
		"ad_size":                event.AdSize,
		"creative_id":            event.CreativeId,
		"ad_domain":              event.AdDomain,
		"imp_id":                 event.ImpId,
		"winning_bid_id":         event.WinningBidId,
		"seat_id":                event.SeatId,
	}

	// Deserialize internal JSON blobs if present
	if len(event.RawBidRequest) > 0 {
		var rawReq interface{}
		if err := json.Unmarshal(event.RawBidRequest, &rawReq); err == nil {
			output["raw_bid_request"] = rawReq
		} else {
			output["raw_bid_request_raw"] = string(event.RawBidRequest)
		}
	}

	if len(event.SspDspResponse) > 0 {
		var rawResp interface{}
		if err := json.Unmarshal(event.SspDspResponse, &rawResp); err == nil {
			output["ssp_dsp_response"] = rawResp
		} else {
			output["ssp_dsp_response_raw"] = string(event.SspDspResponse)
		}
	}

	// Handle OneOf Source field (App/Web)
	if event.GetApp() != nil {
		output["source_app"] = event.GetApp()
	} else if event.GetWeb() != nil {
		output["source_web"] = event.GetWeb()
	}

	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling to JSON: %v\n", err)
		return
	}

	fmt.Println(string(jsonBytes))
}
