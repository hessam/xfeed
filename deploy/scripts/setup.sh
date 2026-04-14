#!/usr/bin/env bash
set -euo pipefail

# DNS delegation and routing health checks (INF-05, DEV-01).
# Usage:
#   ./setup.sh <vps_ip> <tunnel_domain> [authoritative_ns]
# Example:
#   ./setup.sh 203.0.113.10 t.example.com ns-server.example.com

VPS_IP="${1:-}"
TUNNEL_DOMAIN="${2:-}"
AUTH_NS="${3:-}"

if [[ -z "${VPS_IP}" || -z "${TUNNEL_DOMAIN}" ]]; then
  echo "Usage: $0 <vps_ip> <tunnel_domain> [authoritative_ns]"
  exit 1
fi

echo "==> [1/4] Checking NS records for ${TUNNEL_DOMAIN}"
dig +short NS "${TUNNEL_DOMAIN}" || true

if [[ -n "${AUTH_NS}" ]]; then
  if ! dig +short NS "${TUNNEL_DOMAIN}" | rg -q "${AUTH_NS}\.?$"; then
    echo "ERROR: expected NS ${AUTH_NS} not found for ${TUNNEL_DOMAIN}"
    exit 1
  fi
fi

echo "==> [2/4] Verifying delegated resolver path via VPS @${VPS_IP}"
R1="$(dig @"${VPS_IP}" "${TUNNEL_DOMAIN}" SOA +noall +comments +answer)"
echo "${R1}"
if ! printf "%s\n" "${R1}" | rg -q "status: NOERROR"; then
  echo "ERROR: delegated SOA query did not return NOERROR"
  exit 1
fi

echo "==> [3/4] Verifying TXT query path for tunnel payload labels"
TEST_LABEL="healthcheck-$(date +%s)"
R2="$(dig @"${VPS_IP}" "${TEST_LABEL}.${TUNNEL_DOMAIN}" TXT +noall +comments +answer)"
echo "${R2}"
if ! printf "%s\n" "${R2}" | rg -q "status: (NOERROR|NXDOMAIN)"; then
  echo "ERROR: TXT tunnel query path is unhealthy"
  exit 1
fi

echo "==> [4/4] Resolver parity checks (1.1.1.1 and 8.8.8.8)"
dig @1.1.1.1 "${TUNNEL_DOMAIN}" NS +short || true
dig @8.8.8.8 "${TUNNEL_DOMAIN}" NS +short || true

echo "SUCCESS: DNS delegation checks passed for ${TUNNEL_DOMAIN}"
