package models

// timeoutError is a custom error type for command timeouts
type TimeoutError struct {
	Stderr  string
	Message string
}

func (e *TimeoutError) Error() string {
	return e.Message + ": " + e.Stderr
}
