package endpoints

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/analytics"
	"github.com/prebid/prebid-server/v3/endpoints/events"
	"github.com/prebid/prebid-server/v3/partners"
	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

// Win Transformer Logic

const (
	rtMacros = "auction_id=${AUCTION_ID}&bid_id=${AUCTION_BID_ID}&imp_id=${AUCTION_IMP_ID}&price=${AUCTION_PRICE}&cur=${AUCTION_CURRENCY}&seat=${AUCTION_SEAT_ID}&ad_id=${AUCTION_AD_ID}"
)

type TrackingConfig struct {
	ExternalURL string
	AccountID   string
	Timestamp   int64
	Integration string
	AuctionID   string // The original SSP request ID
	Seat        string // The bidder name/seat
}

// TransformWinningBid modifies the bid's NURL and AdM to include exchange-specific tracking and AES-encrypted DSP info.
func TransformWinningBid(bid *openrtb2.Bid, ssp partners.SSPInventory, dsp partners.DSPInventory, dspPrice float64, requestPrice float64, tck TrackingConfig) {
	if bid == nil {
		return
	}

	// 1. Prepare Encrypted DSP URL (if exists)
	encryptedDspUrl := ""
	if bid.NURL != "" {
		encryptedDspUrl, _ = cryptoutil.Encrypt(bid.NURL)
	}

	// 2. Prepare Encrypted Internal Payload (Including all crucial IDs to bypass macro reliance)
	payload := fmt.Sprintf("tid=%d&siid=%d&diid=%d&sid=%d&did=%d&dp=%.6f&sp=%.6f&bp=%.6f&aid=%s&bid=%s&imid=%s&seat=%s&adid=%s",
		ssp.TenantID, ssp.SSPInventoryID, dsp.DSPInventoryID, ssp.SSPID, dsp.DSPID, dspPrice, bid.Price, requestPrice,
		tck.AuctionID, bid.ID, bid.ImpID, tck.Seat, bid.AdID)
	encryptedPayload, _ := cryptoutil.Encrypt(payload)

	// 3. Update NURL with custom SSP win URL + RTB Macros + Encrypted DSP Info + Encrypted Payload
	winHost := ssp.WinURL
	if winHost == "" {
		winHost = "https://win.my-exchange.com/event" // Fallback
	}
	bid.NURL = fmt.Sprintf("%s?type=win&durl=%s&pd=%s&%s",
		winHost, url.QueryEscape(encryptedDspUrl), url.QueryEscape(encryptedPayload), rtMacros)

	// 4. Modify AdM (Inject Tracking Pixels for both VAST and HTML)
	impHost := ssp.ImpTrackURL
	if impHost == "" {
		impHost = "https://win.my-exchange.com/event" // Fallback
	}
	pixelUrl := fmt.Sprintf("%s?type=pixel&pd=%s&%s", impHost, url.QueryEscape(encryptedPayload), rtMacros)

	// Exchange-side Viewability URL
	viewUrl := events.GetVastUrlTrackingByType(tck.ExternalURL, bid.ID, dsp.DSPIdentifier, tck.AccountID, tck.Timestamp, tck.Integration, analytics.View, "")

	bid.AdM = modifyAdmEnhanced(bid.AdM, pixelUrl, viewUrl, dsp.DSPIdentifier, tck)

	// 5. Add Click Tracking (if available)
	if ssp.ClickTrackURL != "" {
		clickUrl := fmt.Sprintf("%s?type=click&pd=%s&%s", ssp.ClickTrackURL, url.QueryEscape(encryptedPayload), rtMacros)
		if !dsp.AdmPriceTransparency {
			clickUrl = cleanseDspMacros(clickUrl)
		}
		bid.AdM = injectClickTracker(bid.AdM, clickUrl)
	}

	// 6. Handle Price Transparency (Masking/Cleansing)
	if !dsp.AdmPriceTransparency {
		bid.AdM = cleanseDspMacros(bid.AdM)
	}
}

// modifyAdmEnhanced handles the core injection logic for tracking inside the endpoint directory.
func modifyAdmEnhanced(adm, pixelUrl, viewUrl, bidder string, tck TrackingConfig) string {
	if adm == "" {
		return adm
	}

	// 1. Detect VAST (XML)
	if strings.Contains(adm, "<?xml") || strings.Contains(adm, "<VAST") {
		// Quartile Tracking (as an AdExchange)
		if strings.Contains(adm, "</TrackingEvents>") {
			quartiles := []analytics.VastType{analytics.Start, analytics.FirstQuartile, analytics.MidPoint, analytics.ThirdQuartile, analytics.Complete}
			var qTrackers strings.Builder
			for _, q := range quartiles {
				// Using the actual bid.ID from the auction logic instead of a macro
				url := events.GetVastUrlTrackingByType(tck.ExternalURL, tck.AuctionID+"_"+bidder, bidder, tck.AccountID, tck.Timestamp, tck.Integration, analytics.Vast, q)
				qTrackers.WriteString(fmt.Sprintf("<Tracking event=\"%s\"><![CDATA[%s]]></Tracking>", q, url))
			}
			adm = strings.Replace(adm, "</TrackingEvents>", qTrackers.String()+"</TrackingEvents>", 1)
		}

		// Impression Pixel
		trackingTag := fmt.Sprintf("<Impression><![CDATA[%s]]></Impression>", pixelUrl)
		if strings.Contains(adm, "</Impression>") {
			adm = strings.Replace(adm, "</Impression>", "</Impression>"+trackingTag, 1)
		} else if strings.Contains(adm, "</InLine>") {
			adm = strings.Replace(adm, "</InLine>", trackingTag+"</InLine>", 1)
		}
		return adm
	}

	// 2. Detect Native (JSON)
	if strings.HasPrefix(strings.TrimSpace(adm), "{") {
		var nativeMap map[string]interface{}
		if err := json.Unmarshal([]byte(adm), &nativeMap); err == nil {
			target := nativeMap
			if n, ok := nativeMap["native"].(map[string]interface{}); ok {
				target = n
			}
			// Add imptrackers
			if imps, ok := target["imptrackers"].([]interface{}); ok {
				target["imptrackers"] = append(imps, pixelUrl)
			} else {
				target["imptrackers"] = []interface{}{pixelUrl}
			}
			// Add OMID bridge for Native if possible via jstracker
			target["jstracker"] = fmt.Sprintf("/* OMID Bridge */ var i=new Image();i.src='%s';", viewUrl)

			if newAdm, err := json.Marshal(nativeMap); err == nil {
				return string(newAdm)
			}
		}
	}

	// 3. HTML (Banner) Modification
	pixelHtml := fmt.Sprintf("<img src=\"%s\" width=\"1\" height=\"1\" style=\"display:none;\" />", pixelUrl)
	omidHtml := fmt.Sprintf("<script>/* OMID Viewability Bridge */ setTimeout(function(){ new Image().src='%s'; }, 1000);</script>", viewUrl)

	return adm + pixelHtml + omidHtml
}

// injectClickTracker wraps or appends click tracking depending on AdM content.
func injectClickTracker(adm string, clickUrl string) string {
	if adm == "" {
		return adm
	}
	pixelHtml := fmt.Sprintf("<img src=\"%s\" width=\"1\" height=\"1\" style=\"display:none;\" />", clickUrl)
	return adm + pixelHtml
}

// cleanseDspMacros replaces pricing macros in the DSP's AdM with a masked value.
func cleanseDspMacros(adm string) string {
	if adm == "" {
		return adm
	}
	maskedValue := "MASKED"
	adm = strings.ReplaceAll(adm, "${AUCTION_PRICE}", maskedValue)
	adm = strings.ReplaceAll(adm, "${AUCTION_CURRENCY}", maskedValue)
	return adm
}

func Decrypt(cryptoText string) (string, error) {
	return cryptoutil.Decrypt(cryptoText)
}
