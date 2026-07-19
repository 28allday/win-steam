#!/bin/bash
# run-vm.sh — boot the installed test VM (payload.iso stays attached so
# SNGI.exe is on the D:/E: CD drive inside Windows).
set -euo pipefail
source "$(dirname "$0")/vm-common.sh"
cd "$VMDIR"

[[ -f "$DISK" ]] || { echo "No disk.qcow2 — run ./install-vm.sh first" >&2; exit 1; }
PAYLOAD=payload.iso
[[ -f payload-full.iso ]] && PAYLOAD=payload-full.iso
[[ -f "$PAYLOAD" ]] || ./make-media.sh

start_swtpm
# app.iso carries just SNGI.exe — regenerate cheaply per iteration; the big
# payload ISO (recovery image) stays static.
APPISO=()
[[ -f app.iso ]] && APPISO=(-drive "file=app.iso,media=cdrom")

# shellcheck disable=SC2046
exec qemu-system-x86_64 $(qemu_args) \
  -drive "file=$PAYLOAD,media=cdrom" \
  "${APPISO[@]}" \
  -boot order=c
