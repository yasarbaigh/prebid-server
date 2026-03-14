package endpoints

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/analytics"
	"github.com/prebid/prebid-server/v3/partners"
	"github.com/prebid/prebid-server/v3/util/cryptoutil"
)

// Win Transformer Logic

const (
	rtMacros = "aid=${AUCTION_ID}&bid=${AUCTION_BID_ID}&imid=${AUCTION_IMP_ID}&price=${AUCTION_PRICE}&mbr=${AUCTION_MBR}&cur=${AUCTION_CURRENCY}&seat=${AUCTION_SEAT_ID}&adid=${AUCTION_AD_ID}"

	// Default Tracker Hosts
	DefaultBaseDmn = "https://win.ssp.cd.com"
)

type TrackingConfig struct {
	ExternalURL   string
	AccountID     string
	Timestamp     int64
	Integration   string
	AuctionID     string // The original SSP request ID
	Seat          string // The bidder name/seat
	DeviceType    string
	OS            string
	OSV           string
	Country       string
	AdType        string
	AdSize        string
	SiteAppDomain string
	BundleID      string
	Carrier       string
}

// getPositionalPayload generates a compact pipe-delimited string for high-speed URL reduction.
func getPositionalPayload(ssp partners.SSPInventory, dsp partners.DSPInventory, tck TrackingConfig, bidID, impID, adID string) string {
	ts := time.Now().Unix()
	// Order: ts|tid|sid|siid|did|diid|dt|os|osv|cnt|at|as|dom|bundle|car|aid|bid|imid|seat|adid
	return fmt.Sprintf("%d|%d|%d|%d|%d|%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		ts, ssp.TenantID, ssp.SSPID, ssp.SSPInventoryID, dsp.DSPID, dsp.DSPInventoryID,
		tck.DeviceType, tck.OS, tck.OSV, tck.Country, tck.AdType, tck.AdSize,
		tck.SiteAppDomain, tck.BundleID, tck.Carrier,
		tck.AuctionID, bidID, impID, tck.Seat, adID)
}

// TransformWinningBid modifies the bid's NURL and AdM to include exchange-specific tracking and AES-encrypted DSP info.
func TransformWinningBid(bid *openrtb2.Bid, ssp partners.SSPInventory, dsp partners.DSPInventory, dspPrice float64, requestPrice float64, tck TrackingConfig) {
	if bid == nil {
		return
	}

	// 1. NURL Specific Optimized Parameters (p and d)
	pPayload := getPositionalPayload(ssp, dsp, tck, "", "", "")
	encryptedP, _ := cryptoutil.EncryptCompressed(pPayload)

	encryptedD := ""
	if bid.NURL != "" {
		encryptedD, _ = cryptoutil.EncryptCompressed(bid.NURL)
	}

	// 2. Determine Base Domain and Update NURL
	baseDmn := ssp.WinBaseDmn
	if baseDmn == "" {
		baseDmn = DefaultBaseDmn
	}
	baseDmn = strings.TrimRight(baseDmn, "/")

	winHost := baseDmn + "/e/win"
	bid.NURL = fmt.Sprintf("%s?d=%s&p=%s&%s",
		winHost, url.QueryEscape(encryptedD), url.QueryEscape(encryptedP), rtMacros)

	// 3. Prepare Tracker Payload (p) for imp, view, click, video, omid
	trackPayload := getPositionalPayload(ssp, dsp, tck, bid.ID, bid.ImpID, bid.AdID)
	encryptedTrackP, _ := cryptoutil.EncryptCompressed(trackPayload)

	// Modify AdM (Inject Tracking Pixels for both VAST and HTML)
	impHost := baseDmn + "/t/imp"
	pixelUrl := fmt.Sprintf("%s?p=%s", impHost, url.QueryEscape(encryptedTrackP))

	viewHost := baseDmn + "/t/view"
	viewUrl := fmt.Sprintf("%s?p=%s", viewHost, url.QueryEscape(encryptedTrackP))

	bid.AdM = modifyAdmEnhanced(bid.AdM, pixelUrl, viewUrl, dsp.DSPIdentifier, tck, baseDmn, encryptedTrackP)

	// 5. Add Click Tracking
	clickHost := baseDmn + "/t/clk"
	clickUrl := fmt.Sprintf("%s?p=%s", clickHost, url.QueryEscape(encryptedTrackP))

	// Check transparency: both must be true to avoid masking
	isTransparent := ssp.AdmPriceTransparency && dsp.AdmPriceTransparency

	if !isTransparent {
		clickUrl = cleanseDspMacros(clickUrl)
	}
	bid.AdM = injectClickTracker(bid.AdM, clickUrl)

	// 6. Handle Price Transparency (Masking/Cleansing)
	if !isTransparent {
		bid.AdM = cleanseDspMacros(bid.AdM)
	}

	// 7. Transform LURL
	if bid.LURL != "" {
		// Encrypt original LURL into 'd'
		encryptedLossD, _ := cryptoutil.EncryptCompressed(bid.LURL)

		lossHost := baseDmn + "/e/loss"
		// p query param with all signed values, and 3 specific macros
		bid.LURL = fmt.Sprintf("%s?d=%s&p=%s&aid={AUCTION_ID}&price={AUCTION_PRICE}&loss={AUCTION_LOSS}",
			lossHost, url.QueryEscape(encryptedLossD), url.QueryEscape(encryptedTrackP))
	} else {
		// If DSP didn't provide LURL, ensure it stays empty
		bid.LURL = ""
	}

	// 8. Ensure BURL is empty
	bid.BURL = ""
}

// modifyAdmEnhanced handles the core injection logic for tracking inside the endpoint directory.
func modifyAdmEnhanced(adm, pixelUrl, viewUrl, bidder string, tck TrackingConfig, baseDmn string, encryptedPayload string) string {
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
				videoHost := baseDmn + "/t/video"
				url := fmt.Sprintf("%s?event=%s&p=%s", videoHost, q, url.QueryEscape(encryptedPayload))
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
