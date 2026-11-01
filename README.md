# proxy2vpn

[![CI](https://github.com/yukanoe/proxy2vpn/actions/workflows/ci.yml/badge.svg)](https://github.com/yukanoe/proxy2vpn/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An HTTP proxy that routes each domain one of three ways — **through an L2TP/IPsec VPN**, **direct**, or **blocked** — and brings the VPN up only while it is needed.

```
browser / curl ──► proxy2vpn :7998 ─┬─ *.sub-1.example.com ─► ppp0 ─► L2TP/IPsec ─► VPN server
                                    ├─ *.sub-2.example.com ─► eth0 ─► internet
                                    └─ anything else ───────► 403
```

- **Per-domain routing**: `vpn`, `direct` or `block`, with a configurable default.
- **VPN on demand**: the first request to a vpn domain brings the VPN up and *waits* for it (no failed first request). The VPN is disconnected after `IDLE_TIMEOUT` seconds without VPN traffic. Direct traffic never wakes the VPN.
- **Kill-switch**: vpn connections are bound to `ppp0`; if the VPN drops they fail instead of leaking out. Only the proxy's own marked "direct" sockets may leave through `eth0`.
- **Split DNS**: vpn domains are resolved through the VPN's DNS over `ppp0`, direct domains through the container runtime's DNS.
- Small: one Go binary with no dependencies outside the standard library, plus strongSwan and xl2tpd.

## Quick start

```bash
git clone https://github.com/yukanoe/proxy2vpn.git && cd proxy2vpn
cp .env.example .env     # then edit VPN_* and ROUTE_*
docker compose up -d --build
curl -x http://127.0.0.1:7998 https://ifconfig.me   # needs ifconfig.me in ROUTE_VPN
```

A prebuilt image for `linux/amd64` and `linux/arm64` is published to `ghcr.io/yukanoe/proxy2vpn`. To use it instead of building locally, remove `build: .` from `docker-compose.yml` and change the `image:` line to:

```yaml
    image: ghcr.io/yukanoe/proxy2vpn:latest
```

| Tag | Contents |
|---|---|
| `latest` | Newest release |
| `1.2.3`, `1.2`, `1` | A specific release line |
| `edge` | Tip of `main`, unreleased |
| `sha-<commit>` | One exact commit |

Status of the VPN and the active rules:

```bash
curl http://127.0.0.1:7998/status
```

Point your browser or system proxy at `http://127.0.0.1:7998`.

### Examples

[docs/examples.md](docs/examples.md) has ready-to-run `docker run` and `docker compose` setups, with settings in `.env` or inline, for:

- everything through the VPN (`ROUTE_DEFAULT=vpn`)
- listed domains through the VPN, everything else blocked (`ROUTE_DEFAULT=block`)
- listed domains through the VPN, everything else direct (`ROUTE_DEFAULT=direct`)

## Configuration

All settings are environment variables (see [`.env.example`](.env.example)).

| Variable | Default | Description |
|---|---|---|
| `VPN_SERVER` | — | L2TP server host name or IP |
| `VPN_USER`, `VPN_PASS` | — | PPP credentials (MS-CHAPv2) |
| `VPN_PSK` | empty | IPsec pre-shared key; empty = plain L2TP |
| `VPN_DNS` | VPN-pushed | DNS servers for vpn domains, space-separated |
| `IDLE_TIMEOUT` | `300` | Seconds without VPN traffic before disconnecting |
| `VPN_CONNECT_TIMEOUT` | `30` | Seconds to wait for the VPN to come up |
| `ROUTE_VPN` | empty | Domains routed through the VPN |
| `ROUTE_DIRECT` | empty | Domains routed directly |
| `ROUTE_BLOCK` | empty | Domains rejected with 403 |
| `ROUTE_DEFAULT` | `block` | Action for domains that match nothing: `block`, `direct` or `vpn` |
| `DIRECT_DNS` | runtime DNS | DNS servers for direct domains |
| `PROXY_PORT` | `7998` | Proxy port, inside the container and on `127.0.0.1` of the host (Compose) |
| `LOG_REQUESTS` | `1` | Log each request with its route |

### Domain rules

Lists are comma-separated.

| Entry | Matches |
|---|---|
| `.example.com` or `*.example.com` | `example.com` and every subdomain |
| `example.com` | `example.com` only |

The longest matching entry wins, so `ROUTE_DIRECT=.example.com` with `ROUTE_VPN=.vpn.example.com` sends `a.vpn.example.com` through the VPN and everything else under `example.com` direct. If the same entry appears in several lists, `block` wins over `vpn`, and `vpn` over `direct`.

HTTPS requests are routed by the host name in `CONNECT`, so no TLS interception and no certificates are involved.

## Host requirements

The container needs `NET_ADMIN` and `/dev/ppp` (already set in `docker-compose.yml`) and these kernel modules on the host:

| Module | Used for |
|---|---|
| `ppp_generic`, `ppp_async` | pppd |
| `esp4`, `xfrm_user` | IPsec (only when `VPN_PSK` is set) |

The container checks for them at startup and prints the `modprobe` command if any are missing. L2TP itself runs in userspace (xl2tpd), so `l2tp_ppp` is optional.

### WSL2 / Podman machine

The WSL2 kernel ships these modules but does not load them, and it disables on-demand loading of `ppp_async` (`dev.tty.ldisc_autoload=0`). Load them at boot inside the Podman machine:

```bash
podman machine ssh "printf 'ppp_generic\nppp_async\nesp4\nxfrm_user\n' | sudo tee /etc/modules-load.d/proxy2vpn.conf && sudo systemctl restart systemd-modules-load"
```

All WSL2 distributions share one kernel, so loading them from any distribution works.

## How it works

1. `entrypoint.sh` writes the strongSwan, xl2tpd and pppd configuration, installs the kill-switch, starts `charon` and `xl2tpd`, then runs `proxy2vpn`.
2. For a vpn domain, `proxy2vpn` runs `ipsec up`, asks xl2tpd to dial (`c vpn`), waits for `ppp0` to get an address, then dials the target from a socket bound to `ppp0` (`SO_BINDTODEVICE`).
3. For a direct domain it dials from a socket bound to `eth0` with mark `DIRECT_MARK`, which is the only new traffic the kill-switch lets out of `eth0`.
4. Every 5 seconds it checks the traffic on `ppp0`; after `IDLE_TIMEOUT` seconds without traffic or new requests it disconnects.

pppd adds its default route with metric 200, so traffic that is not bound to `ppp0` keeps using `eth0`.

## Limitations

- One VPN at a time.
- IPv4 only (IPv6 is disabled in the container).
- WebSocket over plain `ws://` is not proxied; `wss://` works through `CONNECT`.
- Linux only for now: the proxy relies on `SO_BINDTODEVICE` and `SO_MARK`.

## Development

```bash
cd proxy
gofmt -l .
go vet ./...
go test -race ./...
```

No local Go toolchain? Run the same checks in a container:

```bash
docker run --rm -v "$PWD/proxy:/src" -w /src golang:1.23-bookworm sh -c "go vet ./... && go test -race ./..."
```

## License

[MIT](LICENSE)
