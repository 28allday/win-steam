#!/bin/bash
# verify-usb.sh — after SNGI flashes the fake USB stick inside the VM, check
# from the host that usb.img actually contains a SteamOS installer.
# Run with the VM POWERED OFF. Needs sudo (loop mount).
set -euo pipefail
cd "$(dirname "$0")"

[[ -f usb.img ]] || { echo "no usb.img" >&2; exit 1; }
[[ $EUID -eq 0 ]] || exec sudo "$0" "$@"

LOOP="$(losetup -f --show -P usb.img)"
trap 'losetup -d "$LOOP"' EXIT
echo "== partition table =="
lsblk -o NAME,SIZE,PARTLABEL,FSTYPE "$LOOP"

# blkid -p probes the device directly — lsblk's PARTLABEL column is empty
# until udev has processed the loop partitions, which never happens here.
want="rootfs-A efi-A home"
ROOT=""
labels=""
for p in "$LOOP"p*; do
  n="$(blkid -p -s PART_ENTRY_NAME -o value "$p" 2>/dev/null || true)"
  labels+="$n"$'\n'
  [[ "$n" == rootfs-A ]] && ROOT="$p"
done
ok=1
for w in $want; do
  grep -qx "$w" <<<"$labels" || { echo "MISSING partition: $w"; ok=0; }
done

MNT="$(mktemp -d)"
if [[ -n "$ROOT" ]]; then
  mount -o ro "$ROOT" "$MNT"
  echo "== nvidia driver in image =="
  if compgen -G "$MNT/usr/lib/modules/"*/updates/dkms/nvidia.ko* >/dev/null; then
    echo "OK: nvidia.ko present"
  else
    echo "MISSING: nvidia.ko"; ok=0
  fi
  grep -q 'blacklist nouveau' "$MNT/etc/modprobe.d/99-nvidia-patch.conf" 2>/dev/null \
    && echo "OK: modprobe conf" || { echo "MISSING: modprobe conf"; ok=0; }
  [[ -f "$MNT/usr/lib/steamos-nvidia/repatch.sh" ]] \
    && echo "OK: self-heal machinery" || echo "note: no self-heal (non-default update mode?)"
  umount "$MNT"
fi
rmdir "$MNT"

[[ $ok -eq 1 ]] && echo "== VERIFY PASSED ==" || { echo "== VERIFY FAILED =="; exit 1; }
