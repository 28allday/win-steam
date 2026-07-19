#!/bin/bash
# vm-common.sh — shared QEMU config for the SNGI test VM. Sourced, not run.

VMDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DISK="$VMDIR/disk.qcow2"          # Windows system disk (100G sparse)
USBIMG="$VMDIR/usb.img"           # fake 16GB USB stick (raw, sparse)
VARS="$VMDIR/OVMF_VARS.fd"
CODE=/usr/share/edk2/x64/OVMF_CODE.4m.fd
TPMDIR="$VMDIR/tpm"

start_swtpm() {
  mkdir -p "$TPMDIR"
  pkill -f "swtpm.*$TPMDIR" 2>/dev/null || true
  swtpm socket --tpm2 --tpmstate "dir=$TPMDIR" \
    --ctrl "type=unixio,path=$TPMDIR/sock" --daemon
}

# Common QEMU arguments. -cpu host passes AMD-V through (nested virt is
# enabled on this host), which is what lets WSL2 run inside the guest.
qemu_args() {
  echo -enable-kvm -machine q35 -cpu host -smp 12 -m 12288 \
    -monitor "unix:$VMDIR/monitor.sock,server,nowait" \
    -qmp "unix:$VMDIR/qmp.sock,server,nowait" \
    -drive "if=pflash,format=raw,readonly=on,file=$CODE" \
    -drive "if=pflash,format=raw,file=$VARS" \
    -chardev "socket,id=chrtpm,path=$TPMDIR/sock" \
    -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0 \
    -drive "file=$DISK,if=none,id=sysdisk,format=qcow2,discard=unmap" \
    -device ahci,id=ahci -device ide-hd,drive=sysdisk,bus=ahci.0 \
    -device qemu-xhci,id=xhci \
    -drive "file=$USBIMG,if=none,id=usbstick,format=raw" \
    -device usb-storage,bus=xhci.0,drive=usbstick,removable=on \
    -nic user,model=e1000 \
    -vga std -display gtk \
    -usb -device usb-tablet \
    -rtc base=localtime
}
