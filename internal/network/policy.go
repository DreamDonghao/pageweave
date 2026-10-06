package network

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// Policy validates destinations. DNS validation does not pin Chromium connections.
type Policy struct {
	Resolver   *net.Resolver
	FixtureURL string
}

// Parse accepts only absolute HTTP(S) URLs without credentials.
func Parse(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("absolute HTTP(S) URL without credentials required")
	}
	if u.Port() != "" {
		if _, e := net.LookupPort("tcp", u.Port()); e != nil {
			return nil, fmt.Errorf("invalid port")
		}
	}
	return u, nil
}

var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48")}

// Public excludes private, local, multicast and reserved address ranges.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range denied {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// Check validates every resolved address within the caller budget.
func (p Policy) Check(ctx context.Context, raw string) error {
	u, e := Parse(raw)
	if e != nil {
		return e
	}
	if p.FixtureURL != "" {
		f, e := Parse(p.FixtureURL)
		if e == nil && u.Scheme == f.Scheme && u.Host == f.Host {
			return nil
		}
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if ip, e := netip.ParseAddr(host); e == nil {
		if !Public(ip) {
			return fmt.Errorf("destination denied")
		}
		return nil
	}
	r := p.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	ips, e := r.LookupNetIP(ctx, "ip", host)
	if e != nil {
		return fmt.Errorf("DNS lookup: %w", e)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no DNS addresses")
	}
	for _, ip := range ips {
		if !Public(ip) {
			return fmt.Errorf("destination denied")
		}
	}
	return nil
}
