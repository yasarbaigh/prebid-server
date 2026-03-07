package endpoints

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

// Win Transformer Logic

const (
	rtbMacros = "auction_id=${AUCTION_ID}&bid_id=${AUCTION_BID_ID}&imp_id=${AUCTION_IMP_ID}&price=${AUCTION_PRICE}&cur=${AUCTION_CURRENCY}&seat=${AUCTION_SEAT_ID}&ad_id=${AUCTION_AD_ID}"
)

// TransformWinningBid modifies the bid's NURL and AdM to include exchange-specific tracking and AES-encrypted DSP info.
func TransformWinningBid(bid *openrtb2.Bid, ssp partners.SSPInventory, dsp partners.DSPInventory, dspPrice float64, requestPrice float64) {
	if bid == nil {
		return
	}

	// 1. Prepare Encrypted DSP URL (if exists)
	encryptedDspUrl := ""
	if bid.NURL != "" {
		encryptedDspUrl, _ = cryptoutil.Encrypt(bid.NURL)
	}

	// 2. Prepare Encrypted Internal Payload
	payload := fmt.Sprintf("tid=%d&siid=%d&diid=%d&sid=%d&did=%d&dp=%.6f&sp=%.6f&bp=%.6f",
		ssp.TenantID, ssp.SSPInventoryID, dsp.DSPInventoryID, ssp.SSPID, dsp.DSPID, dspPrice, bid.Price, requestPrice)
	encryptedPayload, _ := cryptoutil.Encrypt(payload)

	// 3. Update NURL with custom SSP win URL + RTB Macros + Encrypted DSP Info + Encrypted Payload
	winHost := ssp.WinURL
	if winHost == "" {
		winHost = "https://win.my-exchange.com/event" // Fallback
	}
	bid.NURL = fmt.Sprintf("%s?type=win&durl=%s&pd=%s&%s",
		winHost, url.QueryEscape(encryptedDspUrl), url.QueryEscape(encryptedPayload), rtbMacros)

	// 4. Modify AdM (Inject Tracking Pixels for both VAST and HTML)
	impHost := ssp.ImpTrackURL
	if impHost == "" {
		impHost = "https://win.my-exchange.com/event" // Fallback
	}
	pixelUrl := fmt.Sprintf("%s?type=pixel&pd=%s&%s", impHost, url.QueryEscape(encryptedPayload), rtbMacros)
	bid.AdM = modifyAdm(bid.AdM, pixelUrl)

	// 5. Add Click Tracking (if available)
	if ssp.ClickTrackURL != "" {
		clickUrl := fmt.Sprintf("%s?type=click&pd=%s&%s", ssp.ClickTrackURL, url.QueryEscape(encryptedPayload), rtbMacros)
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

// injectClickTracker wraps or appends click tracking depending on AdM content.
func injectClickTracker(adm string, clickUrl string) string {
	if adm == "" {
		return adm
	}
	// Simplified approach: appending a script/pixel that might handle clicks or just an extra 1x1 for now if it's display
	// In a real scenario, this would involve wrapping the <a> tag link with clickUrl + redirect.
	// For this example, we append it as a comment or extra pixel just to show it's used.
	pixelHtml := fmt.Sprintf("<img src=\"%s\" width=\"1\" height=\"1\" style=\"display:none;\" />", clickUrl)
	return adm + pixelHtml
}

// cleanseDspMacros replaces pricing macros in the DSP's AdM with a masked value.
func cleanseDspMacros(adm string) string {
	if adm == "" {
		return adm
	}
	// Common OpenRTB pricing macros
	maskedValue := "MASKED"
	adm = strings.ReplaceAll(adm, "${AUCTION_PRICE}", maskedValue)
	adm = strings.ReplaceAll(adm, "${AUCTION_CURRENCY}", maskedValue)
	return adm
}

func modifyAdm(adm string, pixelUrl string) string {
	if adm == "" {
		return adm
	}

	// Detect VAST (XML)
	if strings.Contains(adm, "<?xml") || strings.Contains(adm, "<VAST") {
		trackingTag := fmt.Sprintf("<Impression><![CDATA[%s]]></Impression>", pixelUrl)

		// Attempt to inject after existing Impression tags or before closure
		if strings.Contains(adm, "</Impression>") {
			return strings.Replace(adm, "</Impression>", "</Impression>"+trackingTag, 1)
		} else if strings.Contains(adm, "</InLine>") {
			return strings.Replace(adm, "</InLine>", trackingTag+"</InLine>", 1)
		} else if strings.Contains(adm, "</Wrapper>") {
			return strings.Replace(adm, "</Wrapper>", trackingTag+"</Wrapper>", 1)
		}
		// Fallback: append if it looks like XML but structure is unexpected
		return adm + trackingTag
	}

	// Detect Native (JSON)
	if strings.HasPrefix(strings.TrimSpace(adm), "{") {
		var nativeMap map[string]interface{}
		if err := json.Unmarshal([]byte(adm), &nativeMap); err == nil {
			// Check if it has 'native' child or is the native object itself
			target := nativeMap
			if n, ok := nativeMap["native"].(map[string]interface{}); ok {
				target = n
			}

			// Add to imptrackers
			if imps, ok := target["imptrackers"].([]interface{}); ok {
				target["imptrackers"] = append(imps, pixelUrl)
			} else {
				target["imptrackers"] = []string{pixelUrl}
			}

			if newAdm, err := json.Marshal(nativeMap); err == nil {
				return string(newAdm)
			}
		}
	}

	// HTML Modification: Append as a hidden image pixel
	pixelHtml := fmt.Sprintf("<img src=\"%s\" width=\"1\" height=\"1\" style=\"display:none;visibility:hidden;\" />", pixelUrl)
	return adm + pixelHtml
}

func Decrypt(cryptoText string) (string, error) {
	return cryptoutil.Decrypt(cryptoText)
}
