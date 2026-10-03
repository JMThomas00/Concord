package client

import "testing"

// Joining a listed server on 443 confirms over https and saves it as TLS,
// the same rule the hub uses to call the server.
func TestGrapevineSchemeFollowsThePort(t *testing.T) {
	if grapevineScheme(443) != "https" || grapevineScheme(8080) != "http" {
		t.Fatal("wrong scheme")
	}
}
