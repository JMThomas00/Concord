package main

import "testing"

func TestCheckBindHost(t *testing.T) {
	for host, ok := range map[string]bool{
		"":                                 true, // the default, 0.0.0.0
		"0.0.0.0":                          true,
		"192.168.1.10":                     true,
		"::":                               true, // all IPv6 addresses
		"hub.example.com":                  true,
		"https://grapevine.concordchat.cc": false, // what Jordan typed
		"grapevine.concordchat.cc/":        false,
		"0.0.0.0:7777":                     false,
	} {
		if got := checkBindHost(host) == ""; got != ok {
			t.Errorf("%q: accepted %v, want %v (%s)", host, got, ok, checkBindHost(host))
		}
	}
}
