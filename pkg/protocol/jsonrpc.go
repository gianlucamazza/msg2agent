// Package protocol provides wire protocol implementations.
package protocol

import (
	"encoding/json"
	"errors"
)

// JSON-RPC 2.0 standard errors.
var (
	ErrInvalidRequest = errors.New("invalid request")
	ErrMethodNotFound = errors.New("method not found")
	ErrInvalidParams  = errors.New("invalid params")
	ErrInternalError  = errors.New("internal error")
	ErrParseError     = errors.New("parse error")
)

// JSON-RPC 2.0 error codes
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// Security error codes (msg2agent specific)
	CodeAccessDenied        = -32001 // ACL check failed
	CodeRoutingError        = -32002 // Message routing failed
	CodeSignatureInvalid    = -32003 // Signature verification failed
	CodeDecryptionFailed    = -32004 // Message decryption failed
	CodeSenderNotRegistered = -32005 // Sender not registered with relay
	CodeSenderMismatch      = -32006 // Message From field doesn't match client DID
	CodeRateLimited         = -32007 // Rate limit exceeded
)

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSONRPCNotification represents a JSON-RPC 2.0 notification (no ID).
type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// NewRequest creates a new JSON-RPC request.
func NewRequest(id any, method string, params any) (*JSONRPCRequest, error) {
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = data
	}

	return req, nil
}

// NewNotification creates a new JSON-RPC notification.
func NewNotification(method string, params any) (*JSONRPCNotification, error) {
	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		notif.Params = data
	}

	return notif, nil
}

// NewResponse creates a new JSON-RPC success response.
func NewResponse(id any, result any) (*JSONRPCResponse, error) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
	}

	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		resp.Result = data
	}

	return resp, nil
}

// NewErrorResponse creates a new JSON-RPC error response.
func NewErrorResponse(id any, code int, message string, data any) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// Encode encodes a JSON-RPC message to bytes.
func Encode(v any) ([]byte, error) {
	return json.Marshal(v)
}

// DecodeRequest decodes a JSON-RPC request from bytes.
func DecodeRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, ErrParseError
	}
	if req.JSONRPC != "2.0" {
		return nil, ErrInvalidRequest
	}
	if req.Method == "" {
		return nil, ErrInvalidRequest
	}
	return &req, nil
}

// DecodeResponse decodes a JSON-RPC response from bytes.
func DecodeResponse(data []byte) (*JSONRPCResponse, error) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, ErrParseError
	}
	if resp.JSONRPC != "2.0" {
		return nil, ErrInvalidRequest
	}
	return &resp, nil
}

// ParseParams parses the params from a request into the provided value.
func (r *JSONRPCRequest) ParseParams(v any) error {
	if r.Params == nil {
		return nil
	}
	return json.Unmarshal(r.Params, v)
}

// ParseResult parses the result from a response into the provided value.
func (r *JSONRPCResponse) ParseResult(v any) error {
	if r.Result == nil {
		return nil
	}
	return json.Unmarshal(r.Result, v)
}

// IsError returns true if the response contains an error.
func (r *JSONRPCResponse) IsError() bool {
	return r.Error != nil
}

// Error implements the error interface for JSONRPCError.
func (e *JSONRPCError) Error() string {
	return e.Message
}

// Batch represents a batch of JSON-RPC requests.
type Batch struct {
	Requests []*JSONRPCRequest
}

// EncodeBatch encodes a batch of requests.
func EncodeBatch(requests []*JSONRPCRequest) ([]byte, error) {
	return json.Marshal(requests)
}

// DecodeBatch decodes a batch of requests from bytes.
func DecodeBatch(data []byte) ([]*JSONRPCRequest, error) {
	var requests []*JSONRPCRequest
	if err := json.Unmarshal(data, &requests); err != nil {
		return nil, ErrParseError
	}
	return requests, nil
}
