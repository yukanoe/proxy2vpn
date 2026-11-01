// proxy2vpn is an HTTP proxy that routes each domain one of three ways:
// vpn (through L2TP/IPsec), direct (straight out) or block.
// The VPN is brought up only when a vpn domain is requested and dropped when idle.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type tsWriter struct{}

func (tsWriter) Write(p []byte) (int, error) {
	return fmt.Fprintf(os.Stdout, "[%s] %s", time.Now().Format("2006-01-02 15:04:05"), p)
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	n, err := strconv.Atoi(env(key, strconv.Itoa(def)))
	if err != nil {
		log.Fatalf("invalid %s: %v", key, err)
	}
	return n
}

// defaultIface returns the interface of the default route (e.g. eth0) from /proc/net/route.
func defaultIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "eth0"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) > 2 && fs[1] == "00000000" && fs[0] != "ppp0" {
			return fs[0]
		}
	}
	return "eth0"
}

func main() {
	log.SetFlags(0)
	log.SetOutput(tsWriter{})

	def, err := parseAction(env("ROUTE_DEFAULT", "block"))
	if err != nil {
		log.Fatal(err)
	}
	rules := NewRules(os.Getenv("ROUTE_BLOCK"), os.Getenv("ROUTE_VPN"), os.Getenv("ROUTE_DIRECT"), def)

	directIface := env("DIRECT_IFACE", defaultIface())
	directDNS := strings.Fields(os.Getenv("DIRECT_DNS"))
	if len(directDNS) == 0 {
		directDNS = readNameservers("/etc/resolv.conf") // the container runtime's DNS, read before the VPN is up
	}

	vpn := &VPNManager{
		Iface:          env("VPN_IFACE", "ppp0"),
		UsePSK:         os.Getenv("VPN_PSK") != "",
		FixedDNS:       strings.Fields(os.Getenv("VPN_DNS")),
		ConnectTimeout: time.Duration(envInt("VPN_CONNECT_TIMEOUT", 30)) * time.Second,
		IdleTimeout:    time.Duration(envInt("IDLE_TIMEOUT", 300)) * time.Second,
		ActiveBytes:    uint64(envInt("ACTIVE_BYTES", 500)),
	}
	vpn.init()

	vpnRoute := newRoute("vpn", vpn.Iface, 0, vpn.DNS)
	directRoute := newRoute("direct", directIface, envInt("DIRECT_MARK", 1), func() []string { return directDNS })
	vpn.OnDown = vpnRoute.transport.CloseIdleConnections

	p := &Proxy{
		Rules:       rules,
		VPN:         vpn,
		Routes:      map[Action]*route{VPN: vpnRoute, Direct: directRoute},
		LogRequests: env("LOG_REQUESTS", "1") == "1",
		WaitVPN:     vpn.ConnectTimeout + 10*time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go vpn.Watch(ctx)

	addr := ":" + env("PROXY_PORT", "8888")
	srv := &http.Server{Addr: addr, Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		log.Printf("Shutting down...")
		_ = srv.Close()
	}()

	log.Printf("proxy2vpn listening on %s, direct via %s (DNS %s), VPN via %s on demand",
		addr, directIface, strings.Join(directDNS, " "), vpn.Iface)
	log.Printf("Rules: %s", rules)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	vpn.Shutdown()
}
