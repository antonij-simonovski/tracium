package errors

import (
	"errors"
	"fmt"
)

// SpanErrorCode identifies what went wrong with a span.
type SpanErrorCode string

const (
	ErrMissingTraceID   SpanErrorCode = "missing_trace_id"
	ErrMissingSpanID    SpanErrorCode = "missing_span_id"
	ErrInvalidTimestamp SpanErrorCode = "invalid_timestamp"
	ErrUnknownModel     SpanErrorCode = "unknown_model"
	// ErrTokenCountOutOfRange marks a usage count no real call could produce.
	ErrTokenCountOutOfRange SpanErrorCode = "token_count_out_of_range"
	// ErrFieldTooLong marks a client-controlled string past its ingest cap.
	ErrFieldTooLong SpanErrorCode = "field_too_long"
	// ErrUnauthenticated marks a span that reached the pipeline without a
	// verified ingest key.
	ErrUnauthenticated SpanErrorCode = "unauthenticated"
)

// SpanError represents a permanent validation or parsing failure for one span.
// Spans that produce one are sent to the dead-letter store.
type SpanError struct {
	Code    SpanErrorCode
	Message string
	Cause   error
}

func (e *SpanError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("span error [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("span error [%s]: %s", e.Code, e.Message)
}

func (e *SpanError) Unwrap() error { return e.Cause }

// TransientErrorCode identifies the nature of a transient failure.
type TransientErrorCode string

const (
	ErrUserLookupFailed TransientErrorCode = "user_lookup_failed"
)

// TransientError represents a temporary infrastructure failure.
type TransientError struct {
	Code    TransientErrorCode
	Message string
	Cause   error
}

func (e *TransientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("transient error [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("transient error [%s]: %s", e.Code, e.Message)
}

func (e *TransientError) Unwrap() error { return e.Cause }

// InvalidSpan creates a SpanError with no cause.
func InvalidSpan(code SpanErrorCode, msg string) *SpanError {
	return &SpanError{Code: code, Message: msg}
}

// InvalidSpanf creates a SpanError with a formatted message.
func InvalidSpanf(code SpanErrorCode, format string, args ...any) *SpanError {
	return &SpanError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// TransientWrap creates a TransientError that wraps a cause.
func TransientWrap(code TransientErrorCode, msg string, err error) *TransientError {
	return &TransientError{Code: code, Message: msg, Cause: err}
}

// Code returns the code of the SpanError or TransientError in err's chain, or ""
// if there is none.
func Code(err error) string {
	var se *SpanError
	if errors.As(err, &se) {
		return string(se.Code)
	}
	var te *TransientError
	if errors.As(err, &te) {
		return string(te.Code)
	}
	return ""
}
