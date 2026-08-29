#!/bin/sh
set -eu

output_directory="${1:?usage: Collect-ServerEvidence.sh OUTPUT_DIRECTORY}"
mkdir -p "$output_directory"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

uname -a >"$output_directory/$stamp-uname.txt"
ip -details address show >"$output_directory/$stamp-ip-address.txt"
ip -4 route show table all >"$output_directory/$stamp-ipv4-routes.txt"
{
  echo "[public-keys]"
  wg show all public-key
  echo "[listen-ports]"
  wg show all listen-port
  echo "[peers]"
  wg show all peers
  echo "[allowed-ips]"
  wg show all allowed-ips
  echo "[endpoints]"
  wg show all endpoints
  echo "[latest-handshakes]"
  wg show all latest-handshakes
  echo "[transfer]"
  wg show all transfer
} >"$output_directory/$stamp-wireguard-public.txt"
iptables-save >"$output_directory/$stamp-iptables.txt"
cat /proc/sys/net/ipv4/ip_forward >"$output_directory/$stamp-ip-forward.txt"

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$output_directory/$stamp-"* >"$output_directory/$stamp-SHA256SUMS"
fi

echo "Server evidence captured under $output_directory"
