package endpoints

import "github.com/prebid/prebid-server/v3/analytics"

// Video Quartile Enums (Production V7)
// These represent optimized single-character codes for tracking video progress.
const (
	VqStart         = "1"
	VqFirstQuartile = "2"
	VqMidPoint      = "3"
	VqThirdQuartile = "4"
	VqComplete      = "5"
)

// QuartileMapping provides a structure to iterate over all quartiles during tracking injection.
var QuartileMapping = []struct {
	Label analytics.VastType
	Value string
}{
	{analytics.Start, VqStart},
	{analytics.FirstQuartile, VqFirstQuartile},
	{analytics.MidPoint, VqMidPoint},
	{analytics.ThirdQuartile, VqThirdQuartile},
	{analytics.Complete, VqComplete},
}
