#!/bin/bash
# run-build.sh — decompress the already-copied recovery image (the app
# streams it into /build first) and run the installer script. Invoked as:
#   bash /opt/steamos-nvidia/run-build.sh </build/image[.bz2]> [installer flags…]
# Prints "OUTPUT_IMAGE=<path>" on success — the app parses that line.
set -euo pipefail

SRC="${1:?usage: run-build.sh </build/image> [flags]}"; shift
BUILD=/build
mkdir -p "$BUILD"

# WSL2's DNS forwarding can break at any time (VPN joins, reboots, nested
# VMs) — not just during setup. Probe before every build; pin public
# resolvers on failure (mirrors builder-setup.sh, which only runs once).
if ! curl -sfI --max-time 8 https://geo.mirror.pkgbuild.com >/dev/null 2>&1; then
  echo "[win] DNS broken inside WSL — pinning public resolvers (1.1.1.1 / 8.8.8.8)"
  grep -qs 'generateResolvConf=false' /etc/wsl.conf \
    || printf '\n[network]\ngenerateResolvConf=false\n' >> /etc/wsl.conf
  rm -f /etc/resolv.conf
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > /etc/resolv.conf
  curl -sfI --max-time 15 https://geo.mirror.pkgbuild.com >/dev/null 2>&1 \
    || { echo "[win] no network inside WSL even with public DNS — check Windows firewall/VPN and retry" >&2; exit 1; }
  echo "[win] Network OK with pinned resolvers"
fi
[[ -f "$SRC" ]] || { echo "[win] image not found in builder: $SRC" >&2; exit 1; }

case "$SRC" in
  *.img.bz2)
    img="${SRC%.bz2}"
    if [[ -f "$img" ]]; then
      echo "[win] Reusing already-decompressed $(basename "$img")"
    else
      echo "[win] Decompressing $(basename "$SRC") (a few minutes)..."
      bunzip2 -k -f "$SRC"
    fi
    ;;
  *.img)
    img="$SRC"
    ;;
  *)
    echo "[win] Unsupported file type: $(basename "$SRC") (need .img or .img.bz2)" >&2
    exit 1
    ;;
esac

out="${img%.img}-nvidia-usbinstall.img"
bash /opt/steamos-nvidia/steamos-nvidia-installer.sh --workdir "$BUILD/work" "$@" "$img"
[[ -f "$out" ]] || { echo "[win] build finished but $out is missing" >&2; exit 1; }
echo "OUTPUT_IMAGE=$out"
