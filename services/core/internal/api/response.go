package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// errorBody is the single failure shape on the wire. The UI parses JSON for
// every response, so an error must never fall back to text/plain.
type errorBody struct {
	Error   string            `json:"error"`
	Fields  map[string]string `json:"field_errors,omitempty"`
	Details string            `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

func writeFieldErrors(w http.ResponseWriter, fields map[string]string) {
	msg := "request failed validation"
	writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: msg, Fields: fields})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":"response encoding failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// queryInt reads a bounded integer query parameter.
func queryInt(r *http.Request, name string, fallback, min, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return fallback
	}
	return v
}

func clientKey(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.Split(fwd, ",")[0]
	}
	return r.RemoteAddr
}
