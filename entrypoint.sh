#!/bin/bash
# Configure L2TP(/IPsec) and the kill-switch, then run proxy2vpn (HTTP proxy with per-domain routing).
# proxy2vpn brings the VPN up when a vpn domain is requested and drops it when idle.
set -u

: "${VPN_SERVER:?VPN_SERVER is required}"
: "${VPN_USER:?VPN_USER is required}"
: "${VPN_PASS:?VPN_PASS is required}"
VPN_PSK="${VPN_PSK:-}"                   # empty = plain L2TP (no IPsec)
export DIRECT_MARK="${DIRECT_MARK:-1}"   # mark of "direct" sockets; the kill-switch lets only this mark out of eth0

log() { echo "[$(date '+%F %T')] $*"; }

# ---------- Kernel modules (a container cannot modprobe) ----------
missing=""
[ -c /dev/ppp ] || missing="$missing ppp_generic"
# pppd runs PPP on xl2tpd's pty, which needs the ppp line discipline (module ppp_async).
# WSL2 sets dev.tty.ldisc_autoload=0, so the kernel will not load it on demand.
grep -q '^ppp ' /proc/tty/ldiscs 2>/dev/null || missing="$missing ppp_async"
if [ -n "$VPN_PSK" ]; then
  for m in esp4 xfrm_user; do
    [ -d "/sys/module/$m" ] || log "Warning: module $m not loaded yet, IPsec may fail"
  done
fi
if [ -n "$missing" ]; then
  log "Missing kernel modules:$missing"
  log "On the host run: modprobe -a$missing"
  log "WSL/Podman machine: podman machine ssh \"sudo modprobe -a$missing\""
  sleep 30   # avoid a tight restart loop
  exit 1
fi

SERVER_IP=$(getent ahostsv4 "$VPN_SERVER" | awk '{print $1; exit}')
[ -n "$SERVER_IP" ] || { log "Cannot resolve $VPN_SERVER"; exit 1; }
GW=$(ip route | awk '/^default/ {print $3; exit}')
ETH=$(ip route | awk '/^default/ {print $5; exit}')
export DIRECT_IFACE="$ETH"
log "VPN server $SERVER_IP, gateway $GW ($ETH)"

# ---------- Configuration ----------
mkdir -p /var/run/xl2tpd /etc/ppp
touch /var/run/xl2tpd/l2tp-control

cat > /etc/xl2tpd/xl2tpd.conf <<EOF
[global]
port = 1701
[lac vpn]
lns = $SERVER_IP
pppoptfile = /etc/ppp/options.l2tpd.client
length bit = yes
EOF

# Default route via ppp0 with metric 200 (above eth0): normal traffic still uses eth0,
# only proxy2vpn sockets bound to ppp0 use this route.
cat > /etc/ppp/options.l2tpd.client <<EOF
ipcp-accept-local
ipcp-accept-remote
refuse-eap
refuse-pap
refuse-chap
refuse-mschap
require-mschap-v2
noccp
noauth
noipdefault
unit 0
defaultroute
defaultroute-metric 200
usepeerdns
mtu 1280
mru 1280
connect-delay 5000
lcp-echo-interval 30
lcp-echo-failure 4
name "$VPN_USER"
password "$VPN_PASS"
EOF

cat > /etc/ipsec.conf <<EOF
config setup
conn L2TP
  keyexchange=ikev1
  authby=secret
  type=transport
  left=%defaultroute
  leftprotoport=17/1701
  right=$SERVER_IP
  rightprotoport=17/1701
  ike=aes256-sha1-modp2048,aes128-sha1-modp2048,aes128-sha1-modp1024,3des-sha1-modp1024!
  esp=aes256-sha1,aes128-sha1,3des-sha1!
  auto=add
EOF
echo ": PSK \"$VPN_PSK\"" > /etc/ipsec.secrets
chmod 600 /etc/ipsec.secrets

# ---------- Kill-switch ----------
# Out of eth0 only: traffic to the VPN server, replies on existing connections (proxy
# clients), and proxy2vpn "direct" sockets (marked). "vpn" sockets are bound to ppp0
# and cannot leak.
iptables -F OUTPUT
iptables -F INPUT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -o ppp+ -j ACCEPT
iptables -A OUTPUT -d "$SERVER_IP" -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -o "$ETH" -m mark --mark "$DIRECT_MARK" -j ACCEPT
iptables -A OUTPUT -j REJECT
ip route replace "$SERVER_IP" via "$GW" dev "$ETH"

# ---------- Services ----------
[ -n "$VPN_PSK" ] && ipsec start >/dev/null 2>&1 && sleep 2
xl2tpd -c /etc/xl2tpd/xl2tpd.conf -D &

exec /usr/local/bin/proxy2vpn
