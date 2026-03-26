package partners

import "fmt"

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("%s (Status: %d)", e.Message, e.StatusCode)
}
