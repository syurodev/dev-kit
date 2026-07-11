// Package servermode validates the local-only bind address used by Wails server builds.
package servermode

import (
	"fmt"
	"net"
	"strings"
)

const defaultHost = "127.0.0.1"

// ValidateHost returns a safe loopback bind address. Blank input uses the
// IPv4 loopback default; public and wildcard addresses are not supported.
func ValidateHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return defaultHost, nil
	}
	if strings.EqualFold(host, "localhost") {
		return host, nil
	}

	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return host, nil
	}

	return "", fmt.Errorf("server host %q must be a loopback address", host)
}
