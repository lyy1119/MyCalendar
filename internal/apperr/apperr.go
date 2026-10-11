// Package apperr defines the error taxonomy shared by the service and API
// layers. The API layer maps these values onto HTTP status codes and the
// documented JSON error envelope.
package apperr

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("resource not found")

// FieldError pinpoints a single rejected input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validation reports one or more invalid input fields.
type Validation struct {
	Details []FieldError
}

// Error implements the error interface.
func (e *Validation) Error() string {
	if len(e.Details) == 0 {
		return "validation failed"
	}
	parts := make([]string, 0, len(e.Details))
	for _, d := range e.Details {
		parts = append(parts, fmt.Sprintf("%s: %s", d.Field, d.Message))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Add appends a field error and returns the receiver for fluent building.
func (e *Validation) Add(field, message string) *Validation {
	e.Details = append(e.Details, FieldError{Field: field, Message: message})
	return e
}

// Has reports whether any field error was recorded.
func (e *Validation) Has() bool { return len(e.Details) > 0 }

// Invalid creates a Validation carrying a single field error.
func Invalid(field, message string) *Validation {
	return &Validation{Details: []FieldError{{Field: field, Message: message}}}
}

// BadParam reports a malformed request parameter (path, query or body shape).
type BadParam struct {
	Param   string
	Message string
}

// Error implements the error interface.
func (e *BadParam) Error() string {
	if e.Param == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Param, e.Message)
}

// Param creates a BadParam.
func Param(param, message string) *BadParam {
	return &BadParam{Param: param, Message: message}
}
