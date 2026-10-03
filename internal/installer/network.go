package installer

import "net"

// LANAddress is this computer's address on the local network (for "people
// can connect at..."), or "" when it has none.
func LANAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			if ipn.IP.IsPrivate() {
				return ipn.IP.String()
			}
			if fallback == "" {
				fallback = ipn.IP.String()
			}
		}
	}
	return fallback
}

// PortFree reports whether nothing on this computer is listening on port.
func PortFree(port string) bool {
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return false
	}
	l.Close()
	return true
}
