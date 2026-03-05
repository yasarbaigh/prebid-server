package endpoints

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v3/partners"
)

// Win Transformer Logic

// Hardcoded complex key (32 bytes for AES-256)
const (
	aesKey    = "a-very-complex-and-secret-key-32b" // 32 characters for AES-256
	rtbMacros = "auction_id=${AUCTION_ID}&bid_id=${AUCTION_BID_ID}&imp_id=${AUCTION_IMP_ID}&price=${AUCTION_PRICE}&cur=${AUCTION_CURRENCY}&seat=${AUCTION_SEAT_ID}&ad_id=${AUCTION_AD_ID}"
)

// TransformWinningBid modifies the bid's NURL and AdM to include exchange-specific tracking and AES-encrypted DSP info.
func TransformWinningBid(bid *openrtb2.Bid, ssp partners.SSPInventory, dsp partners.DSPInventory) {
	if bid == nil {
		return
	}

	// 1. Prepare Encrypted DSP URL (if exists)
	encryptedDspUrl := ""
	if bid.NURL != "" {
		encryptedDspUrl = encrypt(bid.NURL)
	}

	// 2. Update NURL with custom SSP win URL + RTB Macros + Encrypted DSP Info
	winHost := ssp.WinURL
	if winHost == "" {
		winHost = "https://win.my-exchange.com/event" // Fallback
	}
	bid.NURL = fmt.Sprintf("%s?type=win&dsp_id=%d&dsp_name=%s&url=%s&%s",
		winHost, dsp.DSPID, dsp.Name, encryptedDspUrl, rtbMacros)

	// 3. Modify AdM (Inject Tracking Pixels for both VAST and HTML)
	impHost := ssp.ImpTrackURL
	if impHost == "" {
		impHost = "https://win.my-exchange.com/event" // Fallback
	}
	pixelUrl := fmt.Sprintf("%s?type=pixel&dsp_id=%d&%s", impHost, dsp.DSPID, rtbMacros)
	bid.AdM = modifyAdm(bid.AdM, pixelUrl)

	// 3.5 Add Click Tracking (if available)
	if ssp.ClickTrackURL != "" {
		clickUrl := fmt.Sprintf("%s?type=click&dsp_id=%d&%s", ssp.ClickTrackURL, dsp.DSPID, rtbMacros)
		if !dsp.AdmPriceTransparency {
			clickUrl = cleanseDspMacros(clickUrl)
		}
		bid.AdM = injectClickTracker(bid.AdM, clickUrl)
	}

	// 4. Handle Price Transparency (Masking/Cleansing)
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

func encrypt(plaintext string) string {
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return ""
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ""
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext)
}

// Decrypt reverses the AES-GCM encryption and returns the original plaintext.
func Decrypt(cryptoText string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
