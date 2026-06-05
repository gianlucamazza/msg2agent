package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// application holds shared dependencies for API handlers.
type application struct {
	store      billing.Store
	adminStore billing.AdminStore // non-nil when store also implements AdminStore
	eventStore billing.EventStore
	relayURL   string
	domain     string
	logger     *slog.Logger
}

// apiRouter builds the mux for /api/dashboard/* routes.
func (app *application) apiRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/me", app.handleMe)
	mux.HandleFunc("/api/dashboard/profile", app.handleProfile)
	mux.HandleFunc("/api/dashboard/keys", app.handleKeys)
	mux.HandleFunc("/api/dashboard/keys/", app.handleKeyByID)
	mux.HandleFunc("/api/dashboard/usage", app.handleUsage)
	mux.HandleFunc("/api/dashboard/usage.csv", app.handleUsageCSV)
	mux.HandleFunc("/api/dashboard/usage/by-tool", app.handleUsageByTool)
	mux.HandleFunc("/api/dashboard/audit/verify", app.handleAuditVerify)
	mux.HandleFunc("/api/dashboard/audit/events", app.handleAuditEvents)
	mux.HandleFunc("/api/dashboard/checkout", app.handleCheckout)
	mux.HandleFunc("/api/dashboard/portal", app.handlePortal)
	mux.HandleFunc("/api/dashboard/oauth-clients", app.handleOAuthClients)
	mux.HandleFunc("/api/dashboard/oauth-clients/", app.handleOAuthClients)
	return mux
}

// requireTenant extracts the tenant from context or writes 401 and returns nil.
func requireTenant(w http.ResponseWriter, r *http.Request) *billing.Tenant {
	t := billing.TenantFromContext(r.Context())
	if t == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
	}
	return t
}

// requireStore returns false and writes 501 if store is nil.
func (app *application) requireStore(w http.ResponseWriter) bool {
	if app.store == nil {
		writeError(w, http.StatusNotImplemented, "billing store not configured")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

// page wraps a paginated list response.
type page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// paginate reads ?limit= and ?offset= from the request, slices items, and
// returns a page envelope ready for JSON encoding.
func paginate[T any](r *http.Request, items []T) page[T] {
	limit, offset := defaultPageLimit, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	total := len(items)
	if offset >= total {
		return page[T]{Items: []T{}, Total: total, Limit: limit, Offset: offset}
	}
	end := min(offset+limit, total)
	return page[T]{Items: items[offset:end], Total: total, Limit: limit, Offset: offset}
}

// --- GET /api/dashboard/me ---

type meResponse struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Email               string              `json:"email"`
	EmailVerified       bool                `json:"email_verified"`
	Plan                billing.Plan        `json:"plan"`
	BillingStatus       string              `json:"billing_status"`
	CurrentPeriodEnd    *time.Time          `json:"current_period_end,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
	Quota               billing.QuotaConfig `json:"quota"`
	DID                 string              `json:"did,omitempty"`
	SigningPublicKey    string              `json:"signing_public_key,omitempty"`
	EncryptionPublicKey string              `json:"encryption_public_key,omitempty"`
}

func (app *application) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	resp := meResponse{
		ID:               t.ID,
		Name:             t.Name,
		Email:            t.Email,
		EmailVerified:    t.EmailVerifiedAt != nil,
		Plan:             t.Plan,
		BillingStatus:    t.BillingStatus,
		CurrentPeriodEnd: t.CurrentPeriodEnd,
		CreatedAt:        t.CreatedAt,
		Quota:            t.Quota,
	}
	if len(t.DIDSeed) == 32 {
		ident, err := billing.DeriveTenantIdentity(app.domain, t.ID, t.DIDSeed)
		if err == nil {
			resp.DID = ident.String()
			resp.SigningPublicKey = base64.StdEncoding.EncodeToString(ident.SigningPublicKey())
			resp.EncryptionPublicKey = base64.StdEncoding.EncodeToString(ident.EncryptionPublicKey())
		} else {
			app.logger.Warn("derive tenant identity", "tenant", t.ID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- GET /api/dashboard/keys & POST /api/dashboard/keys ---

type keyListItem struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	KeyPrefix string     `json:"key_prefix"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type createKeyRequest struct {
	Label string `json:"label"`
}

type createKeyResponse struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

func (app *application) handleKeys(w http.ResponseWriter, r *http.Request) {
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if !app.requireStore(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		keys, err := app.store.ListAPIKeys(t.ID)
		if err != nil {
			app.logger.Error("list API keys", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list keys")
			return
		}
		items := make([]keyListItem, 0, len(keys))
		for _, k := range keys {
			items = append(items, keyListItem{
				ID:        k.ID,
				Label:     k.Name,
				CreatedAt: k.CreatedAt,
				LastUsed:  k.LastUsedAt,
				KeyPrefix: k.Prefix,
				RevokedAt: k.RevokedAt,
			})
		}
		writeJSON(w, http.StatusOK, paginate(r, items))

	case http.MethodPost:
		if !globalKeyCreateLimiter.allow(t.ID) {
			writeRateLimitError(w, globalKeyCreateLimiter.retryAfterSecs(t.ID))
			return
		}
		var req createKeyRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.Label == "" {
			req.Label = "API Key"
		}
		if len(req.Label) > 64 {
			writeError(w, http.StatusBadRequest, "label too long (max 64 characters)")
			return
		}
		plaintext, record, err := billing.GenerateAPIKey(t.ID, req.Label)
		if err != nil {
			app.logger.Error("generate API key", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to generate key")
			return
		}
		if err := app.store.PutAPIKey(record); err != nil {
			app.logger.Error("store API key", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store key")
			return
		}
		writeJSON(w, http.StatusCreated, createKeyResponse{
			ID:    record.ID,
			Key:   plaintext,
			Label: record.Name,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- DELETE/PATCH /api/dashboard/keys/{id} ---

func (app *application) handleKeyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if !app.requireStore(w) {
		return
	}

	keyID := strings.TrimPrefix(r.URL.Path, "/api/dashboard/keys/")
	if keyID == "" {
		writeError(w, http.StatusBadRequest, "key id required")
		return
	}

	// Verify the key belongs to this tenant.
	keys, err := app.store.ListAPIKeys(t.ID)
	if err != nil {
		app.logger.Error("list keys for ownership check", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to verify key ownership")
		return
	}
	found := false
	for _, k := range keys {
		if k.ID == keyID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := app.store.RevokeAPIKey(keyID); err != nil {
			app.logger.Error("revoke API key", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to revoke key")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPatch:
		var req struct {
			Label string `json:"label"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		label := strings.TrimSpace(req.Label)
		if label == "" {
			writeError(w, http.StatusBadRequest, "label must not be empty")
			return
		}
		if len(label) > 64 {
			writeError(w, http.StatusBadRequest, "label too long (max 64 characters)")
			return
		}
		if err := app.store.RenameAPIKey(keyID, label); err != nil {
			app.logger.Error("rename API key", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to rename key")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": keyID, "label": label})
	}
}

// --- GET /api/dashboard/usage?period=YYYY-MM ---

type usageRow struct {
	Period string `json:"period"`
	Event  string `json:"event"`
	Count  int64  `json:"count"`
}

func (app *application) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.eventStore == nil {
		writeJSON(w, http.StatusOK, []usageRow{})
		return
	}

	period := r.URL.Query().Get("period")

	// Use SQL-side filter when available; fall back to in-memory filter.
	type filterer interface {
		ListAggregatesByTenantPeriod(tenantID, period string) ([]billing.UsageSnapshot, error)
	}
	var snapshots []billing.UsageSnapshot
	var err error
	if f, ok := app.eventStore.(filterer); ok {
		snapshots, err = f.ListAggregatesByTenantPeriod(t.ID, period)
	} else {
		var allSnaps []billing.UsageSnapshot
		allSnaps, err = app.eventStore.LoadAggregates()
		if err == nil {
			for _, s := range allSnaps {
				if s.TenantID == t.ID && (period == "" || string(s.Period) == period) {
					snapshots = append(snapshots, s)
				}
			}
		}
	}
	if err != nil {
		app.logger.Error("load aggregates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}
	rows := make([]usageRow, 0, len(snapshots))
	for _, s := range snapshots {
		rows = append(rows, usageRow{
			Period: string(s.Period),
			Event:  string(s.Event),
			Count:  s.Count,
		})
	}
	writeJSON(w, http.StatusOK, paginate(r, rows))
}

// --- POST /api/dashboard/checkout ---

func (app *application) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	app.proxyToRelay(w, r, "/api/billing/checkout")
}

// --- POST /api/dashboard/portal ---

func (app *application) handlePortal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	app.proxyToRelay(w, r, "/api/billing/portal")
}

// proxyToRelay forwards the request body to the relay at the given path.
// An optional service token is added from MSG2AGENT_SERVICE_TOKEN env.
func (app *application) proxyToRelay(w http.ResponseWriter, r *http.Request, path string) {
	if app.relayURL == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "checkout proxy not yet configured — set RELAY_URL",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	target := strings.TrimRight(app.relayURL, "/") + path
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		app.logger.Error("build relay request", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to build relay request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		app.logger.Error("relay request failed", "path", path, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("relay unavailable: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// --- GET /api/dashboard/profile & PATCH /api/dashboard/profile ---

type profileResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

type profilePatchRequest struct {
	Name string `json:"name"`
}

func (app *application) handleProfile(w http.ResponseWriter, r *http.Request) {
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if !app.requireStore(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, profileResponse{
			ID:            t.ID,
			Name:          t.Name,
			Email:         t.Email,
			EmailVerified: t.EmailVerifiedAt != nil,
			CreatedAt:     t.CreatedAt,
		})

	case http.MethodPatch:
		var req profilePatchRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name must not be empty")
			return
		}
		if len(name) > 128 {
			writeError(w, http.StatusBadRequest, "name too long (max 128 characters)")
			return
		}
		// Reload tenant to ensure we update the latest version.
		latest, err := app.store.GetTenant(t.ID)
		if err != nil {
			app.logger.Error("get tenant for profile update", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load tenant")
			return
		}
		latest.Name = name
		if err := app.store.UpdateTenant(latest); err != nil {
			app.logger.Error("update tenant name", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update profile")
			return
		}
		writeJSON(w, http.StatusOK, profileResponse{
			ID:            latest.ID,
			Name:          latest.Name,
			Email:         latest.Email,
			EmailVerified: latest.EmailVerifiedAt != nil,
			CreatedAt:     latest.CreatedAt,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- GET /api/dashboard/usage.csv?period=YYYY-MM ---

func (app *application) handleUsageCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.eventStore == nil {
		writeError(w, http.StatusNotImplemented, "usage data not available")
		return
	}

	period := r.URL.Query().Get("period")

	// Use SQL-side filter when available; fall back to in-memory filter.
	type filterer interface {
		ListAggregatesByTenantPeriod(tenantID, period string) ([]billing.UsageSnapshot, error)
	}
	var snapshots []billing.UsageSnapshot
	var err error
	if f, ok := app.eventStore.(filterer); ok {
		snapshots, err = f.ListAggregatesByTenantPeriod(t.ID, period)
	} else {
		var allSnaps []billing.UsageSnapshot
		allSnaps, err = app.eventStore.LoadAggregates()
		if err == nil {
			for _, s := range allSnaps {
				if s.TenantID == t.ID && (period == "" || string(s.Period) == period) {
					snapshots = append(snapshots, s)
				}
			}
		}
	}
	if err != nil {
		app.logger.Error("load aggregates for CSV", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage.csv"`)
	_, _ = w.Write([]byte("period,event,count\n"))
	for _, s := range snapshots {
		line := fmt.Sprintf("%s,%s,%d\n", s.Period, s.Event, s.Count)
		_, _ = w.Write([]byte(line))
	}
}

// --- GET /api/dashboard/audit/verify ---

type auditVerifyResponse struct {
	TenantID   string `json:"tenant_id"`
	Verified   int64  `json:"verified"`
	Tampered   bool   `json:"tampered"`
	FirstBadID string `json:"first_bad_id,omitempty"`
}

func (app *application) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.adminStore == nil {
		writeError(w, http.StatusNotImplemented, "audit store not available")
		return
	}

	results, err := app.adminStore.VerifyAuditChain(t.ID)
	if err != nil {
		app.logger.Error("verify audit chain", "error", err)
		writeError(w, http.StatusInternalServerError, "audit verification failed")
		return
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusOK, auditVerifyResponse{TenantID: t.ID, Verified: 0})
		return
	}
	res := results[0]
	writeJSON(w, http.StatusOK, auditVerifyResponse{
		TenantID:   res.TenantID,
		Verified:   res.Verified,
		Tampered:   res.Tampered,
		FirstBadID: res.FirstBadID,
	})
}

// --- GET /api/dashboard/audit/events?period=YYYY-MM&limit=N ---

type auditEventItem struct {
	ID        string    `json:"id"`
	Event     string    `json:"event"`
	ToolName  string    `json:"tool_name,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func (app *application) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.adminStore == nil {
		writeError(w, http.StatusNotImplemented, "audit store not available")
		return
	}

	f := billing.EventFilter{TenantID: t.ID, Limit: 500}
	if period := r.URL.Query().Get("period"); period != "" {
		// parse "YYYY-MM" into month boundaries
		var year, month int
		if _, err := fmt.Sscanf(period, "%d-%d", &year, &month); err == nil && month >= 1 && month <= 12 {
			f.From = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			if month == 12 {
				f.To = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
			} else {
				f.To = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
			}
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			f.Limit = n
		}
	}

	events, err := app.adminStore.QueryEvents(f)
	if err != nil {
		app.logger.Error("query audit events", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load audit events")
		return
	}

	items := make([]auditEventItem, 0, len(events))
	for _, ev := range events {
		items = append(items, auditEventItem{
			ID:        ev.ID,
			Event:     ev.Event,
			ToolName:  ev.ToolName,
			RequestID: ev.RequestID,
			Timestamp: ev.Timestamp,
		})
	}
	writeJSON(w, http.StatusOK, paginate(r, items))
}

// --- GET /api/dashboard/usage/by-tool?period=YYYY-MM ---

type toolUsageRow struct {
	ToolName string `json:"tool_name"`
	Count    int64  `json:"count"`
}

func (app *application) handleUsageByTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.adminStore == nil {
		writeJSON(w, http.StatusOK, []toolUsageRow{})
		return
	}

	period := r.URL.Query().Get("period")
	results, err := app.adminStore.QueryToolBreakdown(t.ID, period)
	if err != nil {
		app.logger.Error("query tool breakdown", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load tool breakdown")
		return
	}
	items := make([]toolUsageRow, 0, len(results))
	for _, row := range results {
		items = append(items, toolUsageRow{ToolName: row.ToolName, Count: row.Count})
	}
	writeJSON(w, http.StatusOK, paginate(r, items))
}

// --- GET /api/dashboard/oauth-clients & DELETE /api/dashboard/oauth-clients/{client_id} ---

func (app *application) handleOAuthClients(w http.ResponseWriter, r *http.Request) {
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if app.adminStore == nil {
		writeError(w, http.StatusNotImplemented, "oauth client store not available")
		return
	}

	// Type-assert for the list/revoke methods we added to SQLiteStore.
	type oauthClientStore interface {
		ListOAuthClientsByTenant(tenantID string) ([]billing.OAuthClientSummary, error)
		RevokeOAuthClientForTenant(tenantID, clientID string) error
	}
	cs, ok := app.adminStore.(oauthClientStore)
	if !ok {
		writeError(w, http.StatusNotImplemented, "oauth client management not available")
		return
	}

	switch r.Method {
	case http.MethodGet:
		clients, err := cs.ListOAuthClientsByTenant(t.ID)
		if err != nil {
			app.logger.Error("list oauth clients", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list connected apps")
			return
		}
		if clients == nil {
			clients = []billing.OAuthClientSummary{}
		}
		writeJSON(w, http.StatusOK, clients)

	case http.MethodDelete:
		clientID := strings.TrimPrefix(r.URL.Path, "/api/dashboard/oauth-clients/")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "client_id required")
			return
		}
		if err := cs.RevokeOAuthClientForTenant(t.ID, clientID); err != nil {
			app.logger.Error("revoke oauth client", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to revoke app")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
