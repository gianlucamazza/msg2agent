package protocol

import (
	"encoding/json"
	"testing"
)

// FuzzDecodeMessage fuzzes JSON-RPC message parsing (requests and responses).
func FuzzDecodeMessage(f *testing.F) {
	// Valid request seeds
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":"abc","method":"message.send","params":{"to":"did:key:abc","body":"hello"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"notify","params":null}`))
	// Valid response seeds
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`))
	// Malformed / edge-case seeds
	f.Add([]byte(`{}`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		// Try decoding as a request; errors are expected and not fatal.
		_, _ = DecodeRequest(data)
		// Also try decoding as a response.
		_, _ = DecodeResponse(data)
	})
}

// agentCardFuzz is a minimal representation of an A2A agent card used only
// for fuzz testing JSON parsing within the protocol package.
type agentCardFuzz struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	URL          string   `json:"url"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	DID          string   `json:"did"`
}

// FuzzDecodeAgentCard fuzzes agent card JSON parsing.
func FuzzDecodeAgentCard(f *testing.F) {
	f.Add([]byte(`{"name":"TestAgent","description":"A test agent","url":"https://agent.example.com","version":"0.1.0","did":"did:key:z6Mk..."}`))
	f.Add([]byte(`{"name":"","url":"http://localhost:8080","version":"1.0.0","capabilities":["streaming","push"]}`))
	f.Add([]byte(`{"name":"Agent","did":"did:web:example.com","capabilities":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`null`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		var card agentCardFuzz
		_ = json.Unmarshal(data, &card)
	})
}
