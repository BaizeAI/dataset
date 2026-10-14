package dataset

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// nfsServerAndPort separates the mount server from the NFS service port.
// Keep the original host spelling (including IPv6 brackets) for compatibility.
func nfsServerAndPort(u *url.URL) (string, string, error) {
	server := u.Host
	if u.Hostname() == "" {
		return "", "", fmt.Errorf("NFS URI must include a server")
	}
	// url.URL.Port treats the final numeric segment of an unbracketed IPv6
	// address as a port. Preserve legacy address-only URIs before splitting.
	if addr, err := netip.ParseAddr(server); err == nil && addr.Is6() {
		return server, "", nil
	}
	if strings.Count(server, ":") > 1 && !strings.HasPrefix(server, "[") {
		return "", "", fmt.Errorf("invalid NFS server %q: IPv6 addresses with a port must be bracketed", server)
	}
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(server, ":") {
			return "", "", fmt.Errorf("invalid NFS port: port must not be empty")
		}
		return server, "", nil
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", "", fmt.Errorf("invalid NFS port %q: must be between 1 and 65535", port)
	}
	return strings.TrimSuffix(server, ":"+port), strconv.FormatUint(number, 10), nil
}
