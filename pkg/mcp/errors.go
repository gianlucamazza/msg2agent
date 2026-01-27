package mcp

import "errors"

// Validation errors for MCP tools.
var (
	// DID validation errors
	ErrDIDEmpty         = errors.New("DID is required")
	ErrDIDTooLong       = errors.New("DID exceeds maximum length")
	ErrDIDInvalidFormat = errors.New("DID has invalid format")

	// Method validation errors
	ErrMethodEmpty         = errors.New("method is required")
	ErrMethodTooLong       = errors.New("method exceeds maximum length")
	ErrMethodInvalidFormat = errors.New("method has invalid format")

	// Params validation errors
	ErrParamsTooLarge = errors.New("params exceed maximum size")

	// Task ID validation errors
	ErrTaskIDEmpty   = errors.New("task_id is required")
	ErrTaskIDTooLong = errors.New("task_id exceeds maximum length")

	// Capability validation errors
	ErrCapabilityEmpty     = errors.New("capability name is required")
	ErrCapabilityTooLong   = errors.New("capability name exceeds maximum length")
	ErrCapabilitiesEmpty   = errors.New("at least one capability is required")
	ErrTooManyCapabilities = errors.New("too many capabilities requested")

	// General errors
	ErrInvalidArguments = errors.New("invalid arguments format")
	ErrEncodingFailed   = errors.New("failed to encode result")
)

// MCPError represents a structured MCP tool error.
type MCPError struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Error implements the error interface.
func (e *MCPError) Error() string {
	return e.Message
}

// Common MCP error codes.
const (
	CodeValidation  = 400
	CodeNotFound    = 404
	CodeInternal    = 500
	CodeUnavailable = 503
)

// NewValidationError creates a validation error.
func NewValidationError(msg string) *MCPError {
	return &MCPError{
		Code:      CodeValidation,
		Message:   msg,
		Retryable: false,
	}
}

// NewNotFoundError creates a not found error.
func NewNotFoundError(msg string) *MCPError {
	return &MCPError{
		Code:      CodeNotFound,
		Message:   msg,
		Retryable: false,
	}
}

// NewInternalError creates an internal error.
func NewInternalError(msg string) *MCPError {
	return &MCPError{
		Code:      CodeInternal,
		Message:   msg,
		Retryable: true,
	}
}

// NewUnavailableError creates an unavailable error.
func NewUnavailableError(msg string) *MCPError {
	return &MCPError{
		Code:      CodeUnavailable,
		Message:   msg,
		Retryable: true,
	}
}
