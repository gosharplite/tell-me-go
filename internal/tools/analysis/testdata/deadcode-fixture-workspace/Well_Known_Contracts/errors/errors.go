package errors
type APIError struct{}
func (e APIError) Error() string { return "error" }
