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
	rtMacros = "u=${AUCTION_ID}&b=${AUCTION_BID_ID}&i=${AUCTION_IMP_ID}&p=${AUCTION_PRICE}&c=${AUCTION_CURRENCY}&s=${AUCTION_SEAT_ID}&a=${AUCTION_AD_ID}"

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

// getPositionalPayload generates a tight binary buffer for high-speed URL reduction.
func getPositionalPayload(ssp partners.SSPInventory, dsp partners.DSPInventory, dspPrice float64, tck TrackingConfig, bidID, impID, adID string) []byte {
	p := &cryptoutil.PositionalData{
		Timestamp:      uint32(time.Now().Unix()),
		TenantID:       uint32(ssp.TenantID),
		SSPID:          uint32(ssp.SSPID),
		SSPInventoryID: uint32(ssp.SSPInventoryID),
		DSPID:          uint32(dsp.DSPID),
		DSPInventoryID: uint32(dsp.DSPInventoryID),
		Price:          dspPrice,
		DeviceType:     cryptoutil.GetDeviceTypeEnum(tck.DeviceType),
		OS:             cryptoutil.GetOsEnum(tck.OS),
		OSV:            tck.OSV,
		Country:        tck.Country,
		AdType:         cryptoutil.GetAdTypeEnum(tck.AdType),
		AdSize:         tck.AdSize,
		Domain:         "", // User requested empty website domain
		BundleID:       tck.BundleID,
		Carrier:        tck.Carrier,
		AuctionID:      tck.AuctionID,
		BidID:          bidID,
		ImpID:          impID,
		Seat:           tck.Seat,
		AdID:           adID,
	}
	bytes, _ := p.Pack()
	return bytes
}

// TransformWinningBid modifies the bid's NURL and AdM to include exchange-specific tracking and AES-encrypted DSP info.
func TransformWinningBid(bid *openrtb2.Bid, ssp partners.SSPInventory, dsp partners.DSPInventory, dspPrice float64, requestPrice float64, tck TrackingConfig) {
	if bid == nil {
		return
	}

	// 1. NURL Specific Optimized Parameters (p and d)
	pBytes := getPositionalPayload(ssp, dsp, dspPrice, tck, "", "", "")
	encryptedP, _ := cryptoutil.EncryptBinary(pBytes)

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
	bid.NURL = fmt.Sprintf("%s?d=%s&x=%s&%s",
		winHost, url.QueryEscape(encryptedD), url.QueryEscape(encryptedP), rtMacros)

	// 3. Prepare Tracker Payload (x) for imp, view, click, video, omid
	trackPBytes := getPositionalPayload(ssp, dsp, dspPrice, tck, bid.ID, bid.ImpID, bid.AdID)
	encryptedTrackP, _ := cryptoutil.EncryptBinary(trackPBytes)

	// 4. Prepare Tracker Hosts
	impHost := baseDmn + "/t/imp"
	pixelUrl := fmt.Sprintf("%s?x=%s", impHost, url.QueryEscape(encryptedTrackP))

	admHost := baseDmn + "/t/adm"
	admUrl := fmt.Sprintf("%s?x=%s", admHost, url.QueryEscape(encryptedTrackP))

	viewHost := baseDmn + "/t/view"
	viewUrl := fmt.Sprintf("%s?x=%s", viewHost, url.QueryEscape(encryptedTrackP))

	// 5. Add Click Tracking
	clickHost := baseDmn + "/t/clk"
	clickUrl := fmt.Sprintf("%s?x=%s", clickHost, url.QueryEscape(encryptedTrackP))

	// Check transparency: both must be true to avoid masking
	isTransparent := ssp.AdmPriceTransparency && dsp.AdmPriceTransparency

	if !isTransparent {
		clickUrl = cleanseDspMacros(clickUrl)
	}

	// 6. Inject all trackers into AdM
	bid.AdM = modifyAdmEnhanced(bid.AdM, pixelUrl, admUrl, viewUrl, clickUrl, dsp.DSPIdentifier, tck, baseDmn, encryptedTrackP)

	// 7. Handle Price Transparency (Masking/Cleansing)
	if !isTransparent {
		bid.AdM = cleanseDspMacros(bid.AdM)
	}

	// 7. Transform LURL
	if bid.LURL != "" {
		// Encrypt original LURL into 'd'
		encryptedLossD, _ := cryptoutil.EncryptCompressed(bid.LURL)

		lossHost := baseDmn + "/e/loss"
		// p query param with all signed values, and 3 specific macros
		bid.LURL = fmt.Sprintf("%s?d=%s&p=%s&aid={AUCTION_ID}&mbr={AUCTION_MBR}&loss={AUCTION_LOSS}",
			lossHost, url.QueryEscape(encryptedLossD), url.QueryEscape(encryptedTrackP))
	} else {
		// If DSP didn't provide LURL, ensure it stays empty
		bid.LURL = ""
	}

	// 8. Ensure BURL is empty
	bid.BURL = ""
}

// modifyAdmEnhanced handles the core injection logic for tracking inside the endpoint directory.
func modifyAdmEnhanced(adm, pixelUrl, admUrl, viewUrl, clickUrl, bidder string, tck TrackingConfig, baseDmn string, encryptedPayload string) string {
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
				url := fmt.Sprintf("%s?vq=%s&x=%s", videoHost, q, url.QueryEscape(encryptedPayload))
				qTrackers.WriteString(fmt.Sprintf("<Tracking vq=\"%s\"><![CDATA[%s]]></Tracking>", q, url))
			}
			adm = strings.Replace(adm, "</TrackingEvents>", qTrackers.String()+"</TrackingEvents>", 1)
		}

		// Impression and ADM Pixels
		impTag := fmt.Sprintf("<Impression><![CDATA[%s]]></Impression>", pixelUrl)
		admTag := fmt.Sprintf("<Impression><![CDATA[%s]]></Impression>", admUrl)

		if strings.Contains(adm, "</Impression>") {
			adm = strings.Replace(adm, "</Impression>", "</Impression>"+impTag+admTag, 1)
		} else if strings.Contains(adm, "</InLine>") {
			adm = strings.Replace(adm, "</InLine>", impTag+admTag+"</InLine>", 1)
		}

		// Click Tracking
		clickTag := fmt.Sprintf("<ClickTracking><![CDATA[%s]]></ClickTracking>", clickUrl)
		if strings.Contains(adm, "</VideoClicks>") {
			adm = strings.Replace(adm, "</VideoClicks>", clickTag+"</VideoClicks>", 1)
		} else if strings.Contains(adm, "</Linear>") {
			adm = strings.Replace(adm, "</Linear>", "<VideoClicks>"+clickTag+"</VideoClicks></Linear>", 1)
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
			// Add imptrackers (Impression and ADM)
			if imps, ok := target["imptrackers"].([]interface{}); ok {
				target["imptrackers"] = append(imps, pixelUrl, admUrl)
			} else {
				target["imptrackers"] = []interface{}{pixelUrl, admUrl}
			}
			// Add clicktrackers
			if clicks, ok := target["clicktrackers"].([]interface{}); ok {
				target["clicktrackers"] = append(clicks, clickUrl)
			} else {
				target["clicktrackers"] = []interface{}{clickUrl}
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
	admHtml := fmt.Sprintf("<img src=\"%s\" width=\"1\" height=\"1\" style=\"display:none;\" />", admUrl)
	omidHtml := fmt.Sprintf("<script>/* OMID Viewability Bridge */ setTimeout(function(){ new Image().src='%s'; }, 1000);</script>", viewUrl)

	return adm + pixelHtml + admHtml + omidHtml
}

// injectClickTracker is deprecated. Use modifyAdmEnhanced for format-specific tracking.
func injectClickTracker(adm string, clickUrl string) string {
	return adm
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
