// json.go is the response-writing helper every handler in this step uses,
// so the content type and the encoding failure path are handled once.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as the JSON response body with status. An encoding
// failure after headers are already sent cannot be turned into a clean
// error response — the best this can do is stop writing.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteStored writes a response body that is already JSON, for A3.6's
// replay: the bytes come back from `idempotency_key.response_body`, and
// decoding and re-encoding them would only introduce a way for a replay to
// differ from what was stored.
//
// A replay is SEMANTICALLY the original response, not byte-identical — the
// column is jsonb, which normalizes whitespace and may reorder keys (A3.6).
// Anything that ever signs or hashes a response body must revisit that.
func WriteStored(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
