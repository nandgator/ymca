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
