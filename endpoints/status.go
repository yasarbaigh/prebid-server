package endpoints

import (
	"fmt"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
)

// NewStatusEndpoint returns a handler which writes the given response when the app is ready to serve requests.
func NewStatusEndpoint(response string) httprouter.Handle {
	/*
	// Today, the app always considers itself ready to serve requests.
	if response == "" {
		return func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
			w.WriteHeader(http.StatusNoContent)
		}
	}

	responseBytes := []byte(response)
	return func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		w.Write(responseBytes)
	}
	*/
	return func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		currentTime := time.Now().Format(time.RFC3339)
		fullResponse := fmt.Sprintf("%s. T35 Prebid Server is running, Current Request Time: %s\n", response, currentTime)

		w.Write([]byte(fullResponse))
	}
}
