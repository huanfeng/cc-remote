// Package listen expands host --listen specifications into concrete bind addresses.
package listen

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Split flattens repeated and comma separated --listen values.
func Split(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// Resolve expands lan:PORT (all private IPv4 addresses), all:PORT (0.0.0.0)
// and localhost:PORT (127.0.0.1), removes duplicates, and drops addresses that
// would collide with a wildcard bind on the same port.
//
// Loopback must always be reachable (local tools, the ccr launcher and service
// commands only talk to loopback): when no resolved address is loopback or a
// wildcard, 127.0.0.1:<port of the first entry> is appended and returned as
// added so the caller can log it.
func Resolve(specs []string, lanIPs func() []net.IP) (addrs []string, added string, err error) {
	if lanIPs == nil {
		lanIPs = LANIPs
	}
	var expanded []string
	for _, spec := range specs {
		host, port, err := net.SplitHostPort(spec)
		if err != nil {
			return nil, "", fmt.Errorf("invalid listen address %q: %w", spec, err)
		}
		if port == "" {
			return nil, "", fmt.Errorf("invalid listen address %q: missing port", spec)
		}
		switch strings.ToLower(host) {
		case "lan":
			ips := lanIPs()
			if len(ips) == 0 {
				return nil, "", fmt.Errorf("listen address %q: no private IPv4 address found on this machine", spec)
			}
			for _, ip := range ips {
				expanded = append(expanded, net.JoinHostPort(ip.String(), port))
			}
		case "all", "":
			expanded = append(expanded, net.JoinHostPort("0.0.0.0", port))
		case "localhost":
			expanded = append(expanded, net.JoinHostPort("127.0.0.1", port))
		default:
			if ip := net.ParseIP(host); ip != nil {
				host = ip.String()
			}
			expanded = append(expanded, net.JoinHostPort(host, port))
		}
	}
	wildcard := map[string]bool{}
	for _, addr := range expanded {
		host, port, _ := net.SplitHostPort(addr)
		if IsWildcard(host) {
			wildcard[port] = true
		}
	}
	seen := map[string]bool{}
	local := false
	for _, addr := range expanded {
		host, port, _ := net.SplitHostPort(addr)
		if seen[addr] || (wildcard[port] && !IsWildcard(host)) {
			continue
		}
		if IsWildcard(host) {
			// One wildcard per port; 0.0.0.0 and :: would also collide.
			key := "*:" + port
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		seen[addr] = true
		local = local || IsWildcard(host) || IsLoopback(addr)
		addrs = append(addrs, addr)
	}
	if !local && len(addrs) > 0 {
		_, port, _ := net.SplitHostPort(addrs[0])
		added = net.JoinHostPort("127.0.0.1", port)
		addrs = append(addrs, added)
	}
	return addrs, added, nil
}

// Insecure returns the specs that bind every interface (all:PORT, :PORT,
// 0.0.0.0:PORT, [::]:PORT). They expose the host beyond the LAN and require
// the explicit --allow-insecure-remote opt-in.
func Insecure(specs []string) []string {
	var out []string
	for _, spec := range specs {
		host, _, err := net.SplitHostPort(spec)
		if err != nil {
			continue
		}
		if strings.EqualFold(host, "all") || IsWildcard(host) {
			out = append(out, spec)
		}
	}
	return out
}

// IsWildcard reports whether host binds every interface.
func IsWildcard(host string) bool {
	ip := net.ParseIP(host)
	return host == "" || (ip != nil && ip.IsUnspecified())
}

// IsLoopback reports whether addr (host:port or a listen spec) only accepts local connections.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LocalURL returns the URL a local client should use for one bind address:
// loopback and wildcard binds are reached through loopback.
func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch strings.ToLower(host) {
	case "", "all", "0.0.0.0", "localhost":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// ProbeURLs returns the loopback URLs at which a local client reaches a host
// configured with specs. Only loopback URLs are returned so the access token
// is never sent to another machine; the host always binds loopback (see
// Resolve), falling back to 127.0.0.1 on the first entry's port.
func ProbeURLs(specs []string) []string {
	resolved, _, err := Resolve(specs, nil)
	if err != nil {
		// e.g. lan: with no private address right now; probe the loopback
		// fallback the host would have added for the first entry.
		resolved = nil
		for _, spec := range specs {
			host, port, e := net.SplitHostPort(spec)
			if e != nil || port == "" {
				continue
			}
			if strings.EqualFold(host, "all") || IsWildcard(host) || IsLoopback(spec) {
				resolved = append(resolved, spec)
			} else if len(resolved) == 0 {
				resolved = append(resolved, net.JoinHostPort("127.0.0.1", port))
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, addr := range resolved {
		host, _, _ := net.SplitHostPort(addr)
		if !IsLoopback(addr) && !IsWildcard(host) && !strings.EqualFold(host, "all") {
			continue
		}
		if u := LocalURL(addr); !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// LANIPs lists private (RFC 1918) IPv4 addresses of interfaces that are up.
func LANIPs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := ipnet.IP.To4(); ip != nil && ip.IsPrivate() {
				ips = append(ips, ip)
			}
		}
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].String() < ips[j].String() })
	return ips
}
