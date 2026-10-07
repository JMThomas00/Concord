package hub

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// errNotPublic is returned when a hub that added itself resolves to an
// address that isn't on the public internet.
var errNotPublic = errors.New("address is not public")

// cgnat is the carrier-grade NAT range (RFC 6598), private in practice.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr reports whether ip is reachable on the public internet: not
// loopback, private, link-local, CGNAT, multicast or unspecified.
func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsMulticast() &&
		!ip.IsUnspecified() && !cgnat.Contains(ip)
}

// allowPrivatePeers is a test hook: tests run peer hubs on 127.0.0.1.
var allowPrivatePeers = false

// publicClient is an HTTP client that only connects to public addresses. It
// checks the address actually dialed, after DNS, so a name that resolves (or
// later re-resolves) to a LAN or loopback address is refused. Hubs that add
// themselves are reached only through it: otherwise anyone could make this
// hub send requests into its own network.
func publicClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !allowPrivatePeers && !publicAddr(ip) {
				return errNotPublic
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// A redirect could lead anywhere; peers are expected to answer directly.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// peerClient is the client for talking to the peer hub at url: any client
// for hubs an admin added (they may be on the LAN), publicClient for hubs
// that added themselves.
func (h *Hub) peerClient(url string, timeout time.Duration) *http.Client {
	if h.db.PeerAnnounced(url) {
		return publicClient(timeout)
	}
	return &http.Client{Timeout: timeout}
}
