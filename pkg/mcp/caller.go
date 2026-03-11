package mcp

import (
	"context"
	"encoding/json"

	"github.com/gianluca/msg2agent/pkg/registry"
)

// AgentCaller abstracts the agent methods used by the MCP server.
type AgentCaller interface {
	DID() string
	Send(ctx context.Context, to, method string, params any) (AgentMessage, error)
	CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error)
	Record() *registry.Agent
}

// AgentMessage is the response interface returned by Send.
type AgentMessage interface {
	IsError() bool
	RawBody() json.RawMessage
}
