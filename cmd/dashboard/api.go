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
	eventStore billing.EventStore
	relayURL   string
	domain     string
	logger     *slog.Logger
}

// apiRouter builds the mux for /api/dashboard/* routes.
func (app *application) apiRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/me", app.handleMe)
	mux.HandleFunc("/api/dashboard/keys", app.handleKeys)
	mux.HandleFunc("/api/dashboard/keys/", app.handleKeyByID)
	mux.HandleFunc("/api/dashboard/usage", app.handleUsage)
	mux.HandleFunc("/api/dashboard/checkout", app.handleCheckout)
	mux.HandleFunc("/api/dashboard/portal", app.handlePortal)
	return mux
}

// requireTenant extracts the tenant from context or writes 401 and returns nil.
func requireTenant(w http.ResponseWriter, r *http.Request) *billing.Tenant {
	t := billing.TenantFromContext(r.Context())
	if t == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}
	return t
}

// requireStore returns false and writes 501 if store is nil.
func (app *application) requireStore(w http.ResponseWriter) bool {
	if app.store == nil {
		http.Error(w, `{"error":"billing store not configured"}`, http.StatusNotImplemented)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
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
	end := offset + limit
	if end > total {
		end = total
	}
	return page[T]{Items: items[offset:end], Total: total, Limit: limit, Offset: offset}
}

// --- GET /api/dashboard/me ---

type meResponse struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Email               string              `json:"email"`
	Plan                billing.Plan        `json:"plan"`
	BillingStatus       string              `json:"billing_status"`
	Quota               billing.QuotaConfig `json:"quota"`
	DID                 string              `json:"did,omitempty"`
	SigningPublicKey    string              `json:"signing_public_key,omitempty"`
	EncryptionPublicKey string              `json:"encryption_public_key,omitempty"`
}

func (app *application) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	resp := meResponse{
		ID:            t.ID,
		Name:          t.Name,
		Email:         t.Email,
		Plan:          t.Plan,
		BillingStatus: t.BillingStatus,
		Quota:         t.Quota,
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
			http.Error(w, "failed to list keys", http.StatusInternalServerError)
			return
		}
		items := make([]keyListItem, 0, len(keys))
		for _, k := range keys {
			items = append(items, keyListItem{
				ID:        k.ID,
				Label:     k.Name,
				CreatedAt: k.CreatedAt,
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
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Label == "" {
			req.Label = "API Key"
		}
		if len(req.Label) > 64 {
			http.Error(w, "label too long (max 64 characters)", http.StatusBadRequest)
			return
		}
		plaintext, record, err := billing.GenerateAPIKey(t.ID, req.Label)
		if err != nil {
			app.logger.Error("generate API key", "error", err)
			http.Error(w, "failed to generate key", http.StatusInternalServerError)
			return
		}
		if err := app.store.PutAPIKey(record); err != nil {
			app.logger.Error("store API key", "error", err)
			http.Error(w, "failed to store key", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, createKeyResponse{
			ID:    record.ID,
			Key:   plaintext,
			Label: record.Name,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- DELETE /api/dashboard/keys/{id} ---

func (app *application) handleKeyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t := requireTenant(w, r)
	if t == nil {
		return
	}
	if !app.requireStore(w) {
		return
	}

	// Extract key ID from path: /api/dashboard/keys/{id}
	keyID := strings.TrimPrefix(r.URL.Path, "/api/dashboard/keys/")
	if keyID == "" {
		http.Error(w, "key id required", http.StatusBadRequest)
		return
	}

	// Verify the key belongs to this tenant before revoking.
	keys, err := app.store.ListAPIKeys(t.ID)
	if err != nil {
		app.logger.Error("list keys for revoke check", "error", err)
		http.Error(w, "failed to verify key ownership", http.StatusInternalServerError)
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
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	if err := app.store.RevokeAPIKey(keyID); err != nil {
		app.logger.Error("revoke API key", "error", err)
		http.Error(w, "failed to revoke key", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- GET /api/dashboard/usage?period=YYYY-MM ---

type usageRow struct {
	Period string `json:"period"`
	Event  string `json:"event"`
	Count  int64  `json:"count"`
}

func (app *application) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	snapshots, err := app.eventStore.LoadAggregates()
	if err != nil {
		app.logger.Error("load aggregates", "error", err)
		http.Error(w, "failed to load usage", http.StatusInternalServerError)
		return
	}

	rows := make([]usageRow, 0)
	for _, s := range snapshots {
		if s.TenantID != t.ID {
			continue
		}
		if period != "" && string(s.Period) != period {
			continue
		}
		rows = append(rows, usageRow{
			Period: string(s.Period),
			Event:  string(s.Event),
			Count:  s.Count,
		})
	}
	writeJSON(w, http.StatusOK, paginate(r, rows))
}

// --- POST /api/dashboard/checkout ---

type checkoutReq struct {
	Plan       string `json:"plan"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

func (app *application) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	target := strings.TrimRight(app.relayURL, "/") + path
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		app.logger.Error("build relay request", "error", err)
		http.Error(w, "failed to build relay request", http.StatusInternalServerError)
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
