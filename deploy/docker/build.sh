#!/bin/sh
set -eu
cd "$(dirname "$0")"
image=$(cat image.txt)
docker build --platform linux/amd64 \
  --build-arg "APT_FORCE_IPV4=${REMLINK_APT_FORCE_IPV4:-1}" \
  --build-arg "APT_DEBIAN_MIRROR=${REMLINK_APT_DEBIAN_MIRROR-http://mirrors.tuna.tsinghua.edu.cn/debian}" \
  --build-arg "APT_SECURITY_MIRROR=${REMLINK_APT_SECURITY_MIRROR-http://mirrors.tuna.tsinghua.edu.cn/debian-security}" \
  -t "$image" .
printf 'Built image: %s\n' "$image"
