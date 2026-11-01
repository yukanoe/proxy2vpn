package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const xl2tpControl = "/var/run/xl2tpd/l2tp-control"

// VPNManager brings the VPN up when a request needs it, holds that request until
// the tunnel is ready, and tears the VPN down after IdleTimeout without use.
type VPNManager struct {
	Iface          string
	UsePSK         bool
	FixedDNS       []string
	ConnectTimeout time.Duration
	IdleTimeout    time.Duration
	ActiveBytes    uint64 // bytes on the VPN interface per tick that count as "in use"
	OnDown         func() // e.g. drop idle HTTP connections

	op chan struct{} // lock for connect/disconnect that can be waited on with a context

	mu       sync.Mutex
	up       bool
	active   int
	lastUsed time.Time
	dns      []string
}

func (v *VPNManager) init() {
	v.op = make(chan struct{}, 1)
	v.lastUsed = time.Now()
}

// Acquire marks the VPN as in use and makes sure it is up. Call release when done.
func (v *VPNManager) Acquire(ctx context.Context) (release func(), err error) {
	v.mu.Lock()
	v.active++
	v.lastUsed = time.Now()
	v.mu.Unlock()
	release = sync.OnceFunc(func() {
		v.mu.Lock()
		v.active--
		v.lastUsed = time.Now()
		v.mu.Unlock()
	})
	if err := v.ensure(ctx); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (v *VPNManager) DNS() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dns
}

func (v *VPNManager) ready() bool {
	v.mu.Lock()
	up := v.up
	v.mu.Unlock()
	return up && ifaceIPv4(v.Iface) != ""
}

func (v *VPNManager) ensure(ctx context.Context) error {
	if v.ready() {
		return nil
	}
	select {
	case v.op <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("waiting for VPN: %w", ctx.Err())
	}
	defer func() { <-v.op }()
	if v.ready() { // another request just brought it up
		return nil
	}
	return v.connect()
}

func (v *VPNManager) connect() error {
	v.mu.Lock()
	wasUp := v.up
	v.mu.Unlock()
	if wasUp {
		log.Printf("VPN dropped, reconnecting")
		v.disconnect()
	}

	log.Printf("Connecting VPN...")
	start := time.Now()
	if v.UsePSK {
		if err := ipsecUp(); err != nil {
			return err
		}
	}
	if err := writeControl("c vpn"); err != nil {
		v.disconnect()
		return err
	}
	deadline := time.Now().Add(v.ConnectTimeout)
	for ifaceIPv4(v.Iface) == "" {
		if time.Now().After(deadline) {
			log.Printf("VPN timed out after %s", v.ConnectTimeout)
			v.disconnect()
			return errors.New("VPN connect timeout")
		}
		time.Sleep(200 * time.Millisecond)
	}

	dns := v.FixedDNS
	if len(dns) == 0 {
		dns = readNameservers("/etc/ppp/resolv.conf") // written by pppd (usepeerdns)
	}
	if len(dns) == 0 {
		dns = []string{"8.8.8.8"}
	}
	v.mu.Lock()
	v.up, v.dns, v.lastUsed = true, dns, time.Now()
	v.mu.Unlock()
	log.Printf("VPN up after %.1fs (%s %s, DNS %s)", time.Since(start).Seconds(),
		v.Iface, ifaceIPv4(v.Iface), strings.Join(dns, " "))
	return nil
}

func (v *VPNManager) disconnect() {
	log.Printf("Disconnecting VPN")
	if err := writeControl("d vpn"); err != nil {
		log.Printf("%v", err)
	}
	if v.UsePSK {
		_ = run(15*time.Second, "ipsec", "down", "L2TP")
	}
	for i := 0; i < 50 && ifaceExists(v.Iface); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	v.mu.Lock()
	v.up = false
	v.mu.Unlock()
	if v.OnDown != nil {
		v.OnDown()
	}
}

// Watch disconnects the VPN when idle and cleans up after a drop. Runs until ctx is done.
func (v *VPNManager) Watch(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var lastBytes uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b, ok := ifaceBytes(v.Iface)
		v.mu.Lock()
		if v.up && ok && lastBytes != 0 && b > lastBytes && b-lastBytes > v.ActiveBytes {
			v.lastUsed = time.Now()
		}
		lastBytes = b
		up, idle := v.up, time.Since(v.lastUsed)
		v.mu.Unlock()

		if !up || (ok && idle < v.IdleTimeout) {
			continue
		}
		select {
		case v.op <- struct{}{}: // skip this tick if a connect/disconnect is in progress
		default:
			continue
		}
		v.mu.Lock()
		up, idle = v.up, time.Since(v.lastUsed)
		v.mu.Unlock()
		switch {
		case up && ifaceIPv4(v.Iface) == "":
			log.Printf("VPN dropped, will reconnect on the next request")
			v.disconnect()
		case up && idle >= v.IdleTimeout:
			log.Printf("No traffic for %s, disconnecting VPN", v.IdleTimeout)
			v.disconnect()
		}
		<-v.op
	}
}

// Shutdown disconnects the VPN if it is up (on container stop).
func (v *VPNManager) Shutdown() {
	v.op <- struct{}{}
	defer func() { <-v.op }()
	v.mu.Lock()
	up := v.up
	v.mu.Unlock()
	if up || ifaceExists(v.Iface) {
		v.disconnect()
	}
}

type VPNStatus struct {
	Up       bool     `json:"up"`
	Iface    string   `json:"iface"`
	IP       string   `json:"ip,omitempty"`
	DNS      []string `json:"dns,omitempty"`
	Active   int      `json:"active_connections"`
	IdleSec  int      `json:"idle_seconds"`
	LimitSec int      `json:"idle_timeout_seconds"`
}

func (v *VPNManager) Status() VPNStatus {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := VPNStatus{Up: v.up, Iface: v.Iface, Active: v.active,
		IdleSec: int(time.Since(v.lastUsed).Seconds()), LimitSec: int(v.IdleTimeout.Seconds())}
	if v.up {
		s.IP, s.DNS = ifaceIPv4(v.Iface), v.dns
	}
	return s
}

// ---------- helpers ----------

func writeControl(cmd string) error {
	f, err := os.OpenFile(xl2tpControl, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", xl2tpControl, err)
	}
	defer f.Close()
	_, err = f.WriteString(cmd + "\n")
	return err
}

// ipsecUp runs "ipsec up L2TP". It exits 0 even when negotiation fails, so success
// is read from the output; on failure the full output is logged for diagnosis.
func ipsecUp() error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ipsec", "up", "L2TP").CombinedOutput()
	out := strings.TrimSpace(string(b))
	if err == nil && strings.Contains(out, "established successfully") {
		return nil
	}
	for _, l := range strings.Split(out, "\n") {
		log.Printf("ipsec: %s", l)
	}
	return fmt.Errorf("IPsec failed: %s", ipsecReason(out, err))
}

// ipsecReason picks the most telling line from "ipsec up" output.
func ipsecReason(out string, err error) string {
	lines := strings.Split(out, "\n")
	for _, key := range []string{"error notify", "AUTHENTICATION_FAILED", "giving up", "failed"} {
		for _, l := range lines {
			if strings.Contains(l, key) {
				return strings.TrimSpace(l)
			}
		}
	}
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func run(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, lines[len(lines)-1])
	}
	return nil
}

func ifaceExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}

func ifaceIPv4(name string) string {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

func ifaceBytes(name string) (uint64, bool) {
	var total uint64
	for _, f := range []string{"rx_bytes", "tx_bytes"} {
		b, err := os.ReadFile("/sys/class/net/" + name + "/statistics/" + f)
		if err != nil {
			return 0, false
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		total += n
	}
	return total, true
}

func readNameservers(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) >= 2 && fs[0] == "nameserver" {
			out = append(out, fs[1])
		}
	}
	return out
}
