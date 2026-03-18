package endpoints

import (
	"fmt"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/prebid-server/v3/partners"
)

// NewStatusEndpoint returns a handler which writes the given response when the app is ready to serve requests.
func NewStatusEndpoint(response string, pm *partners.Manager) httprouter.Handle {
	return func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		if !pm.IsHealthy() {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		currentTime := time.Now().Format(time.RFC3339)
		fullResponse := fmt.Sprintf("%s. T35 Prebid Server is running, Current Request Time: %s\n", response, currentTime)

		w.Write([]byte(fullResponse))
	}
}
