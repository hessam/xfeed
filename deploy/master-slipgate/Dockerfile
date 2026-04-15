FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git 
WORKDIR /src

# 1. Build SlipGate (dnstm)
RUN git clone https://github.com/anonvector/slipgate.git /src/slipgate && \
    cd /src/slipgate && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dnstm ./main.go

# 2. Build VayDNS Server
RUN git clone https://github.com/net2share/vaydns.git /src/vaydns && \
    cd /src/vaydns && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vaydns-server ./vaydns-server

# 2b. Build DNSTT Server
RUN git clone https://github.com/anonvector/dnstt.git /src/dnstt && \
    cd /src/dnstt/dnstt-server && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dnstt-server ./main.go

# 3. Build Xfeed Server (Our logic)
COPY server /src/xfeed
RUN cd /src/xfeed && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/xfeed-server ./cmd/thefeed

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata 

# Copy binaries
COPY --from=builder /out/dnstm /usr/local/bin/dnstm
COPY --from=builder /out/vaydns-server /usr/local/bin/vaydns-server
COPY --from=builder /out/dnstt-server /usr/local/bin/dnstt-server
COPY --from=builder /out/xfeed-server /usr/local/bin/xfeed-server

# Create config directories
RUN mkdir -p /etc/slipgate /etc/slipgate/keys /var/lib/thefeed

# Entrypoint script using unquoted heredoc to avoid build-time expansion
COPY <<'EOF' /entrypoint.sh
#!/bin/sh

# 1. Ensure keys exist
if [ ! -f /etc/slipgate/keys/vaydns.priv ]; then
    /usr/local/bin/vaydns-server -gen-key > /etc/slipgate/keys/vaydns_keys.txt
    grep 'privkey' /etc/slipgate/keys/vaydns_keys.txt | awk '{print $2}' > /etc/slipgate/keys/vaydns.priv
    grep 'pubkey' /etc/slipgate/keys/vaydns_keys.txt | awk '{print $2}' > /etc/slipgate/keys/vaydns.pub
fi
if [ ! -f /etc/slipgate/keys/dnstt.priv ]; then
    /usr/local/bin/dnstt-server -gen-key -privkey-file /etc/slipgate/keys/dnstt.priv -pubkey-file /etc/slipgate/keys/dnstt.pub
    echo "DNSTT Keys Generated."
fi

# 2. Write SlipGate config
cat <<CONF > /etc/slipgate/config.json
{
  "listen": { "address": ":53" },
  "tunnels": [
    {
      "tag": "vaydns",
      "transport": "vaydns",
      "domain": "v.${THEFEED_DOMAIN}",
      "port": 5310,
      "enabled": true,
      "vaydns": {
        "record_type": "txt",
        "private_key": "$(cat /etc/slipgate/keys/vaydns.priv)"
      }
    },
    {
      "tag": "dnstt",
      "transport": "dnstt",
      "domain": "vpn.${THEFEED_DOMAIN}",
      "port": 5311,
      "enabled": true,
      "dnstt": {
        "private_key": "/etc/slipgate/keys/dnstt.priv"
      }
    },
    {
      "tag": "xfeed",
      "transport": "external",
      "domain": "${THEFEED_DOMAIN}",
      "port": 5300,
      "enabled": true
    }
  ],
  "route": {
    "mode": "multi",
    "default": "xfeed"
  }
}
CONF

# Start Xfeed Backend FIRST so it's ready before the router tries to reach it
echo "Starting Xfeed Backend on :5300..."
/usr/local/bin/xfeed-server -listen=:5300 -channels-file="${CHANNELS_FILE}" -x-rss-file="${X_RSS_FILE}" -db="${DB_FILE}" -domain="${THEFEED_DOMAIN}" &

# Start SOCKS5 proxy for VPN traffic (correct subcommand: socks serve)
echo "Starting SOCKS5 proxy on :1080..."
/usr/local/bin/dnstm socks serve --addr 127.0.0.1 --port 1080 &

# Start tunnel workers
echo "Starting DNSTT worker on :5311..."
/usr/local/bin/dnstt-server -privkey-file /etc/slipgate/keys/dnstt.priv "vpn.${THEFEED_DOMAIN}" 127.0.0.1:5311 127.0.0.1:1080 &

echo "Starting VayDNS worker on :5310..."
/usr/local/bin/vaydns-server -udp 127.0.0.1:5310 -privkey-file /etc/slipgate/keys/vaydns.priv -domain "v.${THEFEED_DOMAIN}" -upstream 127.0.0.1:1080 &

# Wait for all workers to bind their ports before the router starts forwarding
sleep 2

echo "Starting SlipGate DNS Router on :53..."
exec /usr/local/bin/dnstm dnsrouter serve
EOF

RUN chmod +x /entrypoint.sh

EXPOSE 53/udp
ENTRYPOINT ["/entrypoint.sh"]
