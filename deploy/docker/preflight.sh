#!/bin/sh
set -eu

if [ ! -c /dev/net/tun ]; then
  echo "RemLink preflight: /dev/net/tun is missing; map the TUN device into the container" >&2
  exit 1
fi

probe_interface="rl-wg-probe"
cleanup() {
  ip link delete dev "$probe_interface" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

if probe_error="$(ip link add dev "$probe_interface" type wireguard 2>&1)"; then
  :
else
  echo "RemLink preflight: kernel WireGuard or CAP_NET_ADMIN is unavailable" >&2
  if [ -n "$probe_error" ]; then
    echo "RemLink preflight: ip link error: $probe_error" >&2
  fi
  exit 1
fi
cleanup
trap - EXIT INT TERM

if [ "$(cat /proc/sys/net/ipv4/ip_forward)" != "1" ]; then
  echo "RemLink preflight: net.ipv4.ip_forward must be 1" >&2
  exit 1
fi

exec "$@"
