package contracts

import (
	"fmt"
	"net/http"
)

// Error is the transport-agnostic error returned by the application layer. It
// carries the HTTP status the API should surface so handlers stay dumb.
type Error struct {
	Status  int
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}

	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func NewError(status int, message string, err error) *Error {
	return &Error{Status: status, Message: message, Err: err}
}

func BadRequest(message string, err error) *Error {
	return NewError(http.StatusBadRequest, message, err)
}

func BadGateway(message string, err error) *Error {
	return NewError(http.StatusBadGateway, message, err)
}

func Internal(message string, err error) *Error {
	return NewError(http.StatusInternalServerError, message, err)
}
