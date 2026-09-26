package domain

import (
	"errors"
	"fmt"
)

// Code is a stable error code. The API maps codes to HTTP statuses; never change the meaning
// of an existing code.
type Code string

const (
	CodeUnsupportedTarget    Code = "unsupported_target"
	CodeIllegalTransition    Code = "illegal_transition"
	CodeBudgetExhausted      Code = "budget_exhausted"
	CodeValidationFailed     Code = "validation_failed"
	CodeInvalidSpecification Code = "invalid_specification"
	CodeRunCancelled         Code = "run_cancelled"
	CodeNotFound             Code = "not_found"
	CodeConcurrentRunWrite   Code = "concurrent_run_write"
)

// Error is a domain error with a stable code.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

// Errorf builds a domain error with a formatted message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the code of the first domain error in err's chain.
func CodeOf(err error) (Code, bool) {
	var de *Error
	if errors.As(err, &de) {
		return de.Code, true
	}
	return "", false
}
