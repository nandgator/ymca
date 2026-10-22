package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsServer(allowed ...string) (http.Handler, *int) {
	hits := 0
	h := CORS(allowed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	return h, &hits
}

func do(h http.Handler, method, origin string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/t/x/me", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORS_ClosedByDefault(t *testing.T) {
	h, _ := corsServer()
	rec := do(h, "GET", "https://app.example", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("with no allowed origins the response carries Allow-Origin %q", got)
	}
}

func TestCORS_AnAllowedOriginIsEchoed(t *testing.T) {
	h, _ := corsServer("https://app.example")
	rec := do(h, "GET", "https://app.example", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if rec.Header().Get("Access-Control-Expose-Headers") != RequestIDHeader {
		t.Error("the request id is not exposed to the browser")
	}
}

func TestCORS_AnUnlistedOriginGetsNothing(t *testing.T) {
	h, _ := corsServer("https://app.example")
	for _, origin := range []string{"https://evil.example", "https://app.example.evil", "null"} {
		rec := do(h, "GET", origin, nil)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q was allowed: %q", origin, got)
		}
	}
}

func TestCORS_APreflightIsAnsweredBeforeAnyHandler(t *testing.T) {
	h, hits := corsServer("https://app.example")
	rec := do(h, "OPTIONS", "https://app.example", map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization, idempotency-key",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status %d, want 204", rec.Code)
	}
	if *hits != 0 {
		t.Fatal("a preflight reached the handler chain; it carries no credential and must not")
	}
	allow := rec.Header().Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "Idempotency-Key", "Content-Type"} {
		if !containsFold(allow, want) {
			t.Errorf("Allow-Headers %q lacks %s: the browser would strip it", allow, want)
		}
	}
}

func TestCORS_APreflightFromAnUnlistedOriginIsNotAnswered(t *testing.T) {
	h, hits := corsServer("https://app.example")
	rec := do(h, "OPTIONS", "https://evil.example", map[string]string{
		"Access-Control-Request-Method": "POST",
	})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("an unlisted origin's preflight was granted: %v", rec.Header())
	}
	if *hits != 1 {
		t.Fatalf("handler hits = %d: the request should fall through unanswered", *hits)
	}
}

func containsFold(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(indexFold(haystack, needle) >= 0)
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
