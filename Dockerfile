# The Go stage runs on the build machine and cross-compiles, so multi-arch builds
# only emulate the final stage.
FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS build
ARG TARGETOS=linux TARGETARCH
WORKDIR /src
COPY proxy/ .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /proxy2vpn .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      strongswan strongswan-starter xl2tpd ppp \
      iptables iproute2 curl ca-certificates procps \
    && rm -rf /var/lib/apt/lists/* \
    # This hook writes the VPN's DNS into /etc/resolv.conf, which breaks DNS for the
    # direct route. proxy2vpn reads the VPN's DNS from /etc/ppp/resolv.conf instead.
    && rm -f /etc/ppp/ip-up.d/0000usepeerdns /etc/ppp/ip-down.d/0000usepeerdns

COPY --from=build /proxy2vpn /usr/local/bin/proxy2vpn
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 7998
ENTRYPOINT ["/entrypoint.sh"]
