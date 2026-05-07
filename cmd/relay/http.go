package main

import (
	"encoding/json"
	"net/http"
)

type apiError struct {
	Error string `json:"error"`
}

// writeError writes a JSON-encoded error response.
// Used by signup, checkout, and portal handlers so clients always receive JSON,
// not the plain-text "text/plain" body that http.Error produces.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: msg})
}

// writeJSON writes a JSON-encoded response with the given status code.
func writeRelayJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
