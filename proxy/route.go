package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// route is one way out to the internet: an interface, a socket mark and its own DNS.
type route struct {
	name      string
	dialer    *net.Dialer
	resolver  *net.Resolver
	transport *http.Transport // for plain HTTP (non-CONNECT) requests
}

func newRoute(name, iface string, mark int, dns func() []string) *route {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second, Control: socketControl(iface, mark)}
	r := &route{name: name, dialer: d}
	// DNS goes through the same interface/mark as the route. The server address the
	// Go resolver reads from /etc/resolv.conf is ignored in favour of the route's servers.
	r.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var last error = errors.New("no DNS server")
			for _, s := range dns() {
				c, err := d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
				if err == nil {
					return c, nil
				}
				last = err
			}
			return nil, last
		},
	}
	r.transport = &http.Transport{
		DialContext:         r.dial,
		MaxIdleConns:        32,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DisableCompression:  true,
	}
	return r
}

func (r *route) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else if ips, err = r.resolver.LookupIP(ctx, "ip4", host); err != nil {
		return nil, fmt.Errorf("resolve %s via %s: %w", host, r.name, err)
	}
	var last error
	for _, ip := range ips {
		c, err := r.dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}
