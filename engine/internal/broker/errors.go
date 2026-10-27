package broker

import "fmt"

type ErrorCode string

const (
	ErrValidation    ErrorCode = "validation_error"
	ErrAuth          ErrorCode = "auth_error"
	ErrAuthorization ErrorCode = "authorization_error"
	ErrNetwork       ErrorCode = "network_error"
	ErrTLS           ErrorCode = "tls_error"
	ErrBroker        ErrorCode = "broker_error"
	ErrTimeout       ErrorCode = "timeout"
	ErrUnsupported   ErrorCode = "unsupported"
	ErrNotFound      ErrorCode = "not_found"
	ErrLimitExceeded ErrorCode = "limit_exceeded"
	ErrCancelled     ErrorCode = "cancelled"
	ErrInternal      ErrorCode = "internal"
)

type Error struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	Cause     error          `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func NewError(code ErrorCode, message string, retryable bool) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable}
}

func Wrap(code ErrorCode, message string, retryable bool, cause error) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable, Cause: cause}
}
