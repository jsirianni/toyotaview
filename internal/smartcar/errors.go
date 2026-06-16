package smartcar

import "fmt"

type APIError struct {
	Operation string
	Method    string
	URL       string
	Status    int
	RequestID string
	Body      string
	Err       error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Err != nil {
		return fmt.Sprintf("%s %s %s: status=%d request_id=%s: %v", e.Operation, e.Method, e.URL, e.Status, e.RequestID, e.Err)
	}

	return fmt.Sprintf("%s %s %s: status=%d request_id=%s", e.Operation, e.Method, e.URL, e.Status, e.RequestID)
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
