#!/bin/bash
# install-vm.sh — create the test VM and run the unattended Windows install.
# Needs: a Windows 11 ISO in this directory (any name matching Win*.iso or
# win11*.iso), payload.iso (run ./make-media.sh first).
# The install is hands-off (~20-30 min): setup reads autounattend.xml from
# payload.iso, wipes the virtual disk, and lands on an autologon desktop.
set -euo pipefail
source "$(dirname "$0")/vm-common.sh"
cd "$VMDIR"

ISO=""
for f in Win*.iso win*.iso; do
  [[ -f "$f" && "$f" != payload.iso ]] && { ISO="$f"; break; }
done
[[ -n "$ISO" ]] || { echo "No Windows ISO found in $VMDIR — download it first (see README-VMTEST.md)" >&2; exit 1; }
[[ -f payload.iso ]] || { echo "payload.iso missing — run ./make-media.sh first" >&2; exit 1; }

if [[ -f "$DISK" ]]; then
  read -rp "disk.qcow2 exists — wipe and reinstall? [y/N] " a
  [[ "$a" == y* || "$a" == Y* ]] || exit 1
  rm -f "$DISK" "$VARS"
fi

qemu-img create -f qcow2 "$DISK" 100G
[[ -f "$USBIMG" ]] || truncate -s 16G "$USBIMG"
cp /usr/share/edk2/x64/OVMF_VARS.4m.fd "$VARS"

start_swtpm
echo "Booting installer from $ISO — fully unattended, takes ~20-30 min."
echo "(If it drops into the UEFI shell, type 'exit' and pick the DVD from the boot menu.)"
# shellcheck disable=SC2046
qemu-system-x86_64 $(qemu_args) \
  -drive "file=$ISO,media=cdrom" \
  -drive "file=payload.iso,media=cdrom" \
  -boot order=d,menu=on
echo "Install VM exited. Next: ./run-vm.sh"
