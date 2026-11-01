package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type Proxy struct {
	Rules       *Rules
	VPN         *VPNManager
	Routes      map[Action]*route
	LogRequests bool
	WaitVPN     time.Duration
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodConnect && !req.URL.IsAbs() {
		p.serveLocal(w, req)
		return
	}
	host := req.URL.Hostname()
	act := p.Rules.Match(host)
	if p.LogRequests {
		log.Printf("%-6s %s %s", act, req.Method, req.URL.Host)
	}
	if act == Block {
		http.Error(w, "proxy2vpn: blocked domain: "+host, http.StatusForbidden)
		return
	}
	if act == VPN {
		ctx, cancel := context.WithTimeout(req.Context(), p.WaitVPN)
		release, err := p.VPN.Acquire(ctx)
		cancel()
		if err != nil {
			log.Printf("vpn    %s: %v", req.URL.Host, err)
			http.Error(w, "proxy2vpn: VPN unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer release()
	}
	rt := p.Routes[act]
	if req.Method == http.MethodConnect {
		p.tunnel(w, req, rt)
	} else {
		p.forward(w, req, rt)
	}
}

func (p *Proxy) tunnel(w http.ResponseWriter, req *http.Request, rt *route) {
	addr := req.URL.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	up, err := rt.dial(ctx, "tcp", addr)
	cancel()
	if err != nil {
		log.Printf("%-6s %s: %v", rt.name, addr, err)
		http.Error(w, "proxy2vpn: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	cc, brw, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	if _, err := cc.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		cc.Close()
		up.Close()
		return
	}
	// The client may send its TLS ClientHello right after CONNECT; it sits in the buffer.
	if n := brw.Reader.Buffered(); n > 0 {
		b, _ := brw.Reader.Peek(n)
		if _, err := up.Write(b); err != nil {
			cc.Close()
			up.Close()
			return
		}
	}
	pipe(cc, up)
}

// pipe copies both ways. io.Copy between two *net.TCPConn uses splice() on Linux.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	select { // one side closed: give the other side up to 30s
	case <-done:
	case <-time.After(30 * time.Second):
	}
	a.Close()
	b.Close()
}

var hopHeaders = []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func removeHop(h http.Header) {
	for _, f := range h.Values("Connection") {
		for _, k := range strings.Split(f, ",") {
			h.Del(strings.TrimSpace(k))
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func (p *Proxy) forward(w http.ResponseWriter, req *http.Request, rt *route) {
	out := req.Clone(req.Context())
	out.RequestURI = ""
	removeHop(out.Header)
	resp, err := rt.transport.RoundTrip(out)
	if err != nil {
		log.Printf("%-6s %s: %v", rt.name, req.URL.Host, err)
		http.Error(w, "proxy2vpn: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	removeHop(resp.Header)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// serveLocal handles requests addressed to the proxy itself (not proxied), e.g. /status.
func (p *Proxy) serveLocal(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/status" {
		http.Error(w, "proxy2vpn: this is an HTTP proxy. Status: /status", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"vpn": p.VPN.Status(), "rules": p.Rules.String()})
}
