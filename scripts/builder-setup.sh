#!/bin/bash
# builder-setup.sh — prepare the private "steamos-builder" Arch WSL distro.
# Run as root via `wsl -d steamos-builder -u root -- bash -s`. Idempotent.
set -euo pipefail

log() { echo "[setup] $*"; }
die() { echo "[setup] FAIL: $*" >&2; exit 1; }

log "Configuring distro defaults"
# Root by default, no Windows PATH noise, no OOBE user-creation prompt.
cat > /etc/wsl.conf <<'EOF'
[user]
default=root
[interop]
appendWindowsPath=false
EOF
rm -f /etc/wsl-distribution.conf

# WSL2's DNS forwarding is flaky in some environments (VPNs, nested VMs,
# odd firewalls): Windows resolves fine but the distro can't. Probe first;
# on failure pin public resolvers and stop WSL regenerating resolv.conf.
log "Checking network/DNS inside WSL"
if ! curl -sfI --max-time 8 https://geo.mirror.pkgbuild.com >/dev/null 2>&1; then
  log "DNS broken inside WSL — pinning public resolvers (1.1.1.1 / 8.8.8.8)"
  printf '\n[network]\ngenerateResolvConf=false\n' >> /etc/wsl.conf
  rm -f /etc/resolv.conf
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > /etc/resolv.conf
  curl -sfI --max-time 15 https://geo.mirror.pkgbuild.com >/dev/null 2>&1 \
    || die "no network inside WSL even with public DNS — check Windows firewall/VPN and retry"
  log "Network OK with pinned resolvers"
fi

# The WSL2 kernel must provide what the image build needs.
log "Checking WSL2 kernel features"
modprobe btrfs 2>/dev/null || true
modprobe overlay 2>/dev/null || true
modprobe loop 2>/dev/null || true
grep -qw btrfs /proc/filesystems   || die "this WSL2 kernel has no btrfs support — run 'wsl --update' in Windows and retry"
grep -qw overlay /proc/filesystems || die "this WSL2 kernel has no overlayfs support — run 'wsl --update' and retry"
[[ -e /dev/loop-control ]] || die "no loop device support in this WSL2 kernel — run 'wsl --update' and retry"

log "Initialising pacman keyring"
[[ -d /etc/pacman.d/gnupg/private-keys-v1.d ]] || { pacman-key --init && pacman-key --populate archlinux; }

log "Updating base system (first run downloads a few hundred MB)"
pacman -Syu --noconfirm

log "Installing build tools"
pacman -S --noconfirm --needed btrfs-progs e2fsprogs rsync curl kmod zstd python binutils bzip2

log "Verifying tools"
for t in losetup blkid btrfs rsync curl depmod sed awk tar zstd pacman python3 readelf bunzip2 e2fsck debugfs; do
  command -v "$t" >/dev/null || die "missing tool after install: $t"
done

mkdir -p /build /opt/steamos-nvidia
log "Builder ready"
