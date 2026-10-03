package hub

import (
	"net/http/httptest"
	"testing"
)

// X-Forwarded-For can't dodge the join limit; real_ip_header is used only
// when set.
func TestClientIP(t *testing.T) {
	h := &Hub{config: DefaultConfig()}
	r := httptest.NewRequest("POST", "/v1/join", nil)
	r.RemoteAddr = "198.51.100.7:5000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if got := h.clientIP(r); got != "198.51.100.7" {
		t.Fatalf("a forwarded header was trusted: %s", got)
	}
	h.config.RealIPHeader = "CF-Connecting-IP"
	if got := h.clientIP(r); got != "203.0.113.9" {
		t.Fatalf("with the setting: %s", got)
	}
}
