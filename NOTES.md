# SNGI — status & next steps (updated 2026-07-19, session 2)

**FULL E2E PASSED IN THE VM.** Build → flash → host verify → the flashed
usb.img boots in QEMU to the SteamOS recovery desktop with both custom
installer icons. Three product bugs were found and fixed along the way —
all would have hit every real Windows user.

## Where things stand

`dist/SNGI.exe` (11 MB) — current build, all fixes in. VM harness in
`vmtest/` (see `README-VMTEST.md`); the Windows VM is installed and
persistent (user `gn`, password `sngi`), builder distro READY, recovery
image inside the distro. The flashed `vmtest/usb.img` is a *verified good*
SteamOS NVIDIA installer as of today.

## Bugs found this session → all fixed + verified in-VM

6. **WSL2 kernel can't mount SteamOS home** — `# CONFIG_UNICODE is not set`
   in Microsoft's kernel, home is ext4+`casefold` → mount fails ("wrong fs
   type … missing codepage"). Valve ships ZERO casefolded dirs (checked), so
   the flag is inert: the installer script now falls back to
   e2fsck → `debugfs -R "feature -casefold"` → e2fsck → remount. One line
   replaced (line count kept). fuse2fs was a dead end (refuses casefold).
7. **DNS dies after reboot, not just at setup** — the builder-setup probe/pin
   only ran once; a later VM/Windows reboot broke WSL DNS again. The same
   probe-and-pin (1.1.1.1/8.8.8.8 + generateResolvConf=false) now also runs
   at the top of every `run-build.sh` invocation.
8. **pacman 10 s download timeout** killed driver fetches from Valve's pool
   on slow networking → `--disable-download-timeout` added to `PACOPTS`.

Structural: `app.go` now re-pushes the embedded scripts into the distro
before **every** build (`pushScripts()`), not only during setup — an updated
exe heals an existing builder without "Remove builder…".

`README.md` "Syncing" section now documents all THREE sed transforms vs
upstream (udevadm, home-mount fallback, PACOPTS) — all line-count-preserving
and no-ops on native Arch.

Harness fix: `verify-usb.sh` now probes labels with `blkid -p` (lsblk's
PARTLABEL is empty without udev). vmctl.sh `type` learned | ' " = ; ( ) $ % ~.

## Verified this session

- Casefold fallback fires and logs correctly; output home has NO casefold
  flag, fsck-clean, and contains the patched repair_device.sh + .stock +
  install_to_hd.sh + both desktop icons.
- Driver build completes in WSL (fast — caches from the earlier run).
- Flash step: fake USB detected, double-click-confirm works (confirm state
  times out after ~5 s — click twice quickly when driving via vmctl).
- `verify-usb.sh` PASSED: partitions, nvidia.ko, modprobe conf, self-heal.
- Flashed usb.img boots in QEMU → SteamOS desktop, both NVIDIA icons.

## Still to test

- [ ] Drag-and-drop of the image file (only Browse tested — needs a real
      Explorer drag; hard to fake via QMP)
- [ ] "Remove builder…" button
- [ ] Fresh-VM re-run of the WSL-install flow to see the fixed messages
      (snapshot or reinstall — current VM is past that stage)
- [ ] Real hardware: Windows box + real USB stick + boot it on the RTX rig
- [ ] Code-signing story for SmartScreen (unsigned exe → "Run anyway")
- [ ] Git init + first push — ASK Gavin public/private first (no repo yet)

## To drive the VM again

```bash
cd ~/Projects/win_steam/vmtest
./run-vm.sh                              # background it
./vmctl.sh key ret; sleep 3; ./vmctl.sh type sngi; ./vmctl.sh key ret
./vmctl.sh key meta_l-r; sleep 2; ./vmctl.sh type "f:/sngi.exe"; ./vmctl.sh key ret
sleep 5; ./vmctl.sh key alt-y            # UAC
# System check → Continue (1032,587) → Browse (755,391) → dblclick file
# (466,217) → Continue (1032,604) → Start build (475,604)
# Flash pane: select drive (755,331), then click Flash (490,609) TWICE
# within ~5 s (armed confirm times out)
./vmctl.sh shot /tmp/x.png
# Rebuild loop: ../build.sh && cp ../dist/SNGI.exe appdir/ && rebuild
# app.iso (xorriso -as mkisofs -o app.iso -V SNGI -J -R appdir) &&
# ./vmctl.sh raw change ide1-cd0 "$PWD/app.iso" → close+relaunch app in VM
```

## Gotchas worth remembering

- wsl.exe management output is UTF-16LE; command output is UTF-8.
- msiexec/dism exit 3010 = success-needs-reboot; WSL MSI needs a real
  Restart (Fast Startup shut-down does NOT complete it).
- QMP absolute clicks only (HMP mouse_move is relative and lands on "Show
  desktop"). vmctl type is UK-layout aware (backslash = `less` key).
- Windows Setup autounattend: UI language must match the ISO variant.
- QEMU boot-from-CD needs Enter-spam right after reset.
- The e2fsprogs package was already in the Arch WSL base image; the
  builder-setup addition is belt-and-braces.
