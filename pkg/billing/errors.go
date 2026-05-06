package billing

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// QuotaErrorData is the structured payload included in quota-exceeded errors
// on both the relay JSON-RPC path and the MCP tool path.
type QuotaErrorData struct {
	Plan        string `json:"plan"`
	Current     int64  `json:"current"`
	Limit       int64  `json:"limit"`
	UpgradeHint string `json:"upgrade_hint"`
}

// FormatQuotaErrorData builds a QuotaErrorData for use in error responses.
// upgrade_hint provides a CLI example showing how to raise the quota.
func FormatQuotaErrorData(plan string, tenantID string, current, limit int64) QuotaErrorData {
	return QuotaErrorData{
		Plan:        plan,
		Current:     current,
		Limit:       limit,
		UpgradeHint: "billing-admin set-plan --tenant " + tenantID + " --plan team",
	}
}

// NewQuotaErrorToolResult builds an MCP CallToolResult for a quota-exceeded error.
// The text content is human/LLM readable; StructuredContent carries machine-readable data.
func NewQuotaErrorToolResult(event UsageEvent, data QuotaErrorData) *mcp.CallToolResult {
	text := fmt.Sprintf(
		"billing quota exceeded: %s limit %d reached (current %d, plan %s). %s",
		event, data.Limit, data.Current, data.Plan, data.UpgradeHint,
	)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: text},
		},
		IsError:           true,
		StructuredContent: data,
	}
}
