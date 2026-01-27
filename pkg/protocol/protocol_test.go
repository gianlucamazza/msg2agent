package protocol

import (
	"encoding/json"
	"testing"
)

func TestNewRequest(t *testing.T) {
	req, err := NewRequest("1", "test.method", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", req.JSONRPC)
	}
	if req.ID != "1" {
		t.Errorf("expected id 1, got %v", req.ID)
	}
	if req.Method != "test.method" {
		t.Errorf("expected method test.method, got %s", req.Method)
	}
}

func TestNewNotification(t *testing.T) {
	notif, err := NewNotification("event", map[string]int{"count": 5})
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	if notif.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", notif.JSONRPC)
	}
	if notif.Method != "event" {
		t.Errorf("expected method event, got %s", notif.Method)
	}
}

func TestNewResponse(t *testing.T) {
	resp, err := NewResponse("1", map[string]bool{"success": true})
	if err != nil {
		t.Fatalf("failed to create response: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if resp.ID != "1" {
		t.Errorf("expected id 1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Error("response should not have error")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse("1", CodeMethodNotFound, "method not found", nil)

	if resp.Result != nil {
		t.Error("error response should not have result")
	}
	if resp.Error == nil {
		t.Fatal("error response should have error")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestEncodeDecodeRequest(t *testing.T) {
	req, _ := NewRequest("123", "test.method", map[string]string{"hello": "world"})

	data, err := Encode(req)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.Method != req.Method {
		t.Errorf("method mismatch: %s != %s", decoded.Method, req.Method)
	}

	var params map[string]string
	if err := decoded.ParseParams(&params); err != nil {
		t.Fatalf("failed to parse params: %v", err)
	}
	if params["hello"] != "world" {
		t.Errorf("params mismatch: %v", params)
	}
}

func TestEncodeDecodeResponse(t *testing.T) {
	resp, _ := NewResponse("456", map[string]int{"count": 42})

	data, err := Encode(resp)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	decoded, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.IsError() {
		t.Error("response should not be error")
	}

	var result map[string]int
	if err := decoded.ParseResult(&result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if result["count"] != 42 {
		t.Errorf("result mismatch: %v", result)
	}
}

func TestDecodeInvalidRequest(t *testing.T) {
	// Invalid JSON
	_, err := DecodeRequest([]byte("not json"))
	if err != ErrParseError {
		t.Errorf("expected parse error, got %v", err)
	}

	// Missing jsonrpc version
	_, err = DecodeRequest([]byte(`{"method": "test"}`))
	if err != ErrInvalidRequest {
		t.Errorf("expected invalid request error, got %v", err)
	}

	// Missing method
	_, err = DecodeRequest([]byte(`{"jsonrpc": "2.0", "id": 1}`))
	if err != ErrInvalidRequest {
		t.Errorf("expected invalid request error, got %v", err)
	}
}

func TestBatch(t *testing.T) {
	req1, _ := NewRequest(1, "method1", nil)
	req2, _ := NewRequest(2, "method2", nil)

	data, err := EncodeBatch([]*JSONRPCRequest{req1, req2})
	if err != nil {
		t.Fatalf("failed to encode batch: %v", err)
	}

	// Verify it's a JSON array
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("batch should be JSON array: %v", err)
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 items, got %d", len(arr))
	}

	// Decode batch
	decoded, err := DecodeBatch(data)
	if err != nil {
		t.Fatalf("failed to decode batch: %v", err)
	}
	if len(decoded) != 2 {
		t.Errorf("expected 2 requests, got %d", len(decoded))
	}
}

func TestJSONRPCErrorInterface(t *testing.T) {
	err := &JSONRPCError{
		Code:    -32601,
		Message: "Method not found",
	}

	if err.Error() != "Method not found" {
		t.Errorf("error message should be %q, got %q", "Method not found", err.Error())
	}
}
