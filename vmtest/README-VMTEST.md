# SNGI VM test harness

Tests the full Windows app — WSL2 setup, build, and USB flash — in a QEMU/KVM
Windows 11 VM on this Linux box. Nested virtualization is required and already
enabled here (`kvm_amd nested=1`); `-cpu host` passes AMD-V into the guest so
WSL2 works inside it.

## One-time setup

1. **Get the Windows 11 ISO** (Mido's automated fetchers are currently broken
   against Microsoft's endpoints, so grab it by hand):
   https://www.microsoft.com/en-gb/software-download/windows11
   → "Download Windows 11 Disk Image (ISO) for x64 devices" → English.
   Save/move the `Win11_*.iso` into this directory.
2. `./make-media.sh` — builds `payload.iso` (SNGI.exe + autounattend.xml).
3. `./install-vm.sh` — unattended install, ~20–30 min, zero clicks.
   Lands on an autologon desktop (user `test`, password `sngi`, edition Home).

## Each test run

- Rebuild the app if needed (`../build.sh`), then `./make-media.sh`.
- `./run-vm.sh` — boots the VM with SNGI.exe on the CD drive and a fake
  16 GB USB stick attached (USB bus — shows up in SNGI's flash list).
- In Windows: open the CD drive, run `SNGI.exe`, click through the wizard.
  The recovery image can be downloaded inside the VM from Valve's page
  (or fetched on the host and dropped into the VM via the CD by adding it
  to `payload/` in make-media.sh — beware: that doubles the ISO build time).
- Power the VM off, then `./verify-usb.sh` — loop-mounts `usb.img` on the
  host and checks the SteamOS partition set, nvidia.ko, modprobe conf and
  self-heal machinery.
- Optional full-circle: boot the flashed stick itself:
  `qemu-system-x86_64 -enable-kvm -cpu host -smp 8 -m 8192 -drive if=pflash,format=raw,readonly=on,file=/usr/share/edk2/x64/OVMF_CODE.4m.fd -drive file=usb.img,format=raw -vga std -display gtk`
  → should reach the SteamOS desktop with the two installer icons.

## What this exercises / what it can't

Covered: system check (incl. the virtualization probe — firmware flag is
visible pre-WSL), WSL install path, builder setup (catalog + mirror
fallback), full driver build in WSL, drag-drop, flash path incl. volume
locking, verify of the written stick.

Not covered: real NVIDIA hardware boot of the *installed* system (the VM has
no GPU) — that stays a physical-machine test, already proven for the script
itself on the RTX 5060 Ti box.

## Notes

- VM files (`disk.qcow2` ~100G sparse, `usb.img` 16G sparse, ISOs) live here
  and are gitignored.
- Expect the in-VM build to be slower than native (nested virt): 30–60 min.
- Delete everything with: `rm -f disk.qcow2 usb.img OVMF_VARS.fd payload.iso; rm -rf tpm payload`
