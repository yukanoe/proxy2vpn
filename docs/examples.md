# Examples

Three common setups, each runnable with `docker run` or `docker compose`, with the settings either in a `.env` file or directly in the command / compose file.

- [Ways to run](#ways-to-run)
- [1. Everything through the VPN](#1-everything-through-the-vpn)
- [2. Listed domains through the VPN, everything else blocked](#2-listed-domains-through-the-vpn-everything-else-blocked)
- [3. Listed domains through the VPN, everything else direct](#3-listed-domains-through-the-vpn-everything-else-direct)
- [Combining rules](#combining-rules)
- [Checking the result](#checking-the-result)

The examples use placeholder credentials (`vpn.example.com`, `myuser`, ...) and the published image `ghcr.io/yukanoe/proxy2vpn:latest`. Replace `image:` with `build: .` to build from a local checkout instead.

## Ways to run

Every setup needs the same container options: `NET_ADMIN`, `/dev/ppp`, IPv6 disabled, and the proxy port published on `127.0.0.1` only. The host also needs a few kernel modules, see [Host requirements](../README.md#host-requirements).

### `docker run` with a `.env` file

```bash
docker run -d --name proxy2vpn \
  --cap-add NET_ADMIN --device /dev/ppp \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --init --restart unless-stopped \
  -p 127.0.0.1:7998:7998 \
  --env-file .env \
  ghcr.io/yukanoe/proxy2vpn:latest
```

### `docker run` with settings in the command

Same options, with one `-e` per variable instead of `--env-file`:

```bash
docker run -d --name proxy2vpn \
  --cap-add NET_ADMIN --device /dev/ppp \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --init --restart unless-stopped \
  -p 127.0.0.1:7998:7998 \
  -e VPN_SERVER=vpn.example.com \
  -e VPN_USER=myuser \
  -e VPN_PASS=mypassword \
  -e VPN_PSK=my_shared_secret \
  -e ROUTE_DEFAULT=vpn \
  ghcr.io/yukanoe/proxy2vpn:latest
```

In PowerShell, end each line with a backtick (`` ` ``) instead of `\`.

### `docker compose` with a `.env` file

`docker-compose.yml`:

```yaml
services:
  proxy2vpn:
    image: ghcr.io/yukanoe/proxy2vpn:latest
    container_name: proxy2vpn
    env_file: .env
    init: true
    cap_add: [NET_ADMIN]
    devices: ["/dev/ppp:/dev/ppp"]
    sysctls:
      net.ipv6.conf.all.disable_ipv6: "1"
    ports:
      - "127.0.0.1:${PROXY_PORT:-7998}:${PROXY_PORT:-7998}"
    restart: unless-stopped
```

```bash
docker compose up -d
```

Compose also reads `.env` for `${PROXY_PORT}`, so changing the port in `.env` changes both sides of the mapping.

### `docker compose` with settings in the compose file

Replace `env_file: .env` with an `environment:` block:

```yaml
services:
  proxy2vpn:
    image: ghcr.io/yukanoe/proxy2vpn:latest
    container_name: proxy2vpn
    init: true
    cap_add: [NET_ADMIN]
    devices: ["/dev/ppp:/dev/ppp"]
    sysctls:
      net.ipv6.conf.all.disable_ipv6: "1"
    ports:
      - "127.0.0.1:7998:7998"
    restart: unless-stopped
    environment:
      VPN_SERVER: vpn.example.com
      VPN_USER: myuser
      VPN_PASS: mypassword
      VPN_PSK: my_shared_secret
      ROUTE_DEFAULT: vpn
```

Keep credentials out of version control: a `.env` file (ignored by git) is safer than putting them in `docker-compose.yml` or in your shell history.

### Stopping

Stop with `docker stop proxy2vpn` or `docker compose down`, which lets proxy2vpn hang up the VPN cleanly. `docker rm -f` kills the container without hanging up; some VPN servers then keep the old session for a while and the next connection times out.

### Changing the port

`PROXY_PORT` sets the port inside the container. With `docker run`, publish the same port: `-e PROXY_PORT=9000 -p 127.0.0.1:9000:9000`.

## 1. Everything through the VPN

Every request goes through the VPN, including requests to bare IP addresses. The VPN comes up on the first request and drops after `IDLE_TIMEOUT` seconds without traffic.

`.env`:

```ini
VPN_SERVER=vpn.example.com
VPN_USER=myuser
VPN_PASS=mypassword
VPN_PSK=my_shared_secret

ROUTE_DEFAULT=vpn
```

`docker run`, settings in the command:

```bash
docker run -d --name proxy2vpn \
  --cap-add NET_ADMIN --device /dev/ppp \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --init --restart unless-stopped \
  -p 127.0.0.1:7998:7998 \
  -e VPN_SERVER=vpn.example.com -e VPN_USER=myuser \
  -e VPN_PASS=mypassword -e VPN_PSK=my_shared_secret \
  -e ROUTE_DEFAULT=vpn \
  ghcr.io/yukanoe/proxy2vpn:latest
```

`docker compose`, settings in the compose file:

```yaml
    environment:
      VPN_SERVER: vpn.example.com
      VPN_USER: myuser
      VPN_PASS: mypassword
      VPN_PSK: my_shared_secret
      ROUTE_DEFAULT: vpn
```

## 2. Listed domains through the VPN, everything else blocked

Only the listed domains are served, through the VPN. Any other request gets `403 Forbidden`, so a misconfigured client cannot use the proxy for anything else. This is the default behaviour (`ROUTE_DEFAULT=block`).

`.env`:

```ini
VPN_SERVER=vpn.example.com
VPN_USER=myuser
VPN_PASS=mypassword
VPN_PSK=my_shared_secret

ROUTE_VPN=.sub-1.example.com,.example.org
ROUTE_DEFAULT=block
```

`docker run`, settings in the command:

```bash
docker run -d --name proxy2vpn \
  --cap-add NET_ADMIN --device /dev/ppp \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --init --restart unless-stopped \
  -p 127.0.0.1:7998:7998 \
  -e VPN_SERVER=vpn.example.com -e VPN_USER=myuser \
  -e VPN_PASS=mypassword -e VPN_PSK=my_shared_secret \
  -e ROUTE_VPN=.sub-1.example.com,.example.org \
  -e ROUTE_DEFAULT=block \
  ghcr.io/yukanoe/proxy2vpn:latest
```

`docker compose`, settings in the compose file:

```yaml
    environment:
      VPN_SERVER: vpn.example.com
      VPN_USER: myuser
      VPN_PASS: mypassword
      VPN_PSK: my_shared_secret
      ROUTE_VPN: .sub-1.example.com,.example.org
      ROUTE_DEFAULT: block
```

## 3. Listed domains through the VPN, everything else direct

The listed domains go through the VPN; everything else leaves directly with your normal IP. Direct traffic never brings the VPN up, so the VPN only runs while you use the listed domains. Use this when the proxy is set system-wide or in a browser and only some sites need the VPN.

`.env`:

```ini
VPN_SERVER=vpn.example.com
VPN_USER=myuser
VPN_PASS=mypassword
VPN_PSK=my_shared_secret

ROUTE_VPN=.sub-1.example.com,.example.org
ROUTE_DEFAULT=direct
```

`docker run`, settings in the command:

```bash
docker run -d --name proxy2vpn \
  --cap-add NET_ADMIN --device /dev/ppp \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --init --restart unless-stopped \
  -p 127.0.0.1:7998:7998 \
  -e VPN_SERVER=vpn.example.com -e VPN_USER=myuser \
  -e VPN_PASS=mypassword -e VPN_PSK=my_shared_secret \
  -e ROUTE_VPN=.sub-1.example.com,.example.org \
  -e ROUTE_DEFAULT=direct \
  ghcr.io/yukanoe/proxy2vpn:latest
```

`docker compose`, settings in the compose file:

```yaml
    environment:
      VPN_SERVER: vpn.example.com
      VPN_USER: myuser
      VPN_PASS: mypassword
      VPN_PSK: my_shared_secret
      ROUTE_VPN: .sub-1.example.com,.example.org
      ROUTE_DEFAULT: direct
```

## Combining rules

`ROUTE_VPN`, `ROUTE_DIRECT` and `ROUTE_BLOCK` can be used together with any default; the longest matching entry wins.

Everything through the VPN except a local site, with ads blocked:

```ini
ROUTE_DEFAULT=vpn
ROUTE_DIRECT=.intranet.example.com
ROUTE_BLOCK=.ads.example.net
```

A domain direct, but one of its subdomains through the VPN:

```ini
ROUTE_DEFAULT=block
ROUTE_DIRECT=.example.com
ROUTE_VPN=.secure.example.com
```

## Checking the result

```bash
# Route and active rules
curl http://127.0.0.1:7998/status

# Exit IP through the proxy (the VPN's IP if the domain is routed to vpn)
curl -x http://127.0.0.1:7998 https://ifconfig.me

# Your own IP, for comparison
curl https://ifconfig.me
```

With `LOG_REQUESTS=1` (the default), `docker logs proxy2vpn` shows the route taken by every request:

```
[2026-11-01 07:43:05] direct CONNECT api.ipify.org:443
[2026-11-01 07:43:05] block  CONNECT www.google.com:443
[2026-11-01 07:43:05] vpn    CONNECT ifconfig.me:443
[2026-11-01 07:43:05] Connecting VPN...
[2026-11-01 07:43:09] VPN up after 4.2s (ppp0 10.246.184.21, DNS 10.246.254.254 8.8.8.8)
```
