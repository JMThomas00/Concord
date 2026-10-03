package server

import (
	"net/http/httptest"
	"testing"
)

// Behind Cloudflare, every request comes from Cloudflare: with
// real_ip_header set, the visitor's own address is used instead (for the
// rate limits and the logs); without it, a header can't claim an address.
func TestRemoteIPBehindAProxy(t *testing.T) {
	defer func() { realIPHeader = "" }()
	r := httptest.NewRequest("POST", "/api/login", nil)
	r.RemoteAddr = "172.68.1.2:44321" // a Cloudflare address
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	realIPHeader = ""
	if got := remoteIP(r); got != "172.68.1.2" {
		t.Fatalf("without the setting: %s", got)
	}
	realIPHeader = "CF-Connecting-IP"
	if got := remoteIP(r); got != "203.0.113.9" {
		t.Fatalf("with the setting: %s", got)
	}
	r.Header.Del("CF-Connecting-IP")
	if got := remoteIP(r); got != "172.68.1.2" {
		t.Fatalf("header missing: %s", got)
	}
}
