# SNGI

**Install real SteamOS on any PC with an NVIDIA RTX graphics card — starting
from Windows.**

A double-clickable Windows app that does exactly what
[steamos-nvidia-installer](https://github.com/28allday/steamos-nvidia-installer)
does on Linux: it takes Valve's official SteamOS recovery image and produces a
bootable USB installer with the NVIDIA open driver baked in (including
self-healing OS updates), then flashes it to a USB stick.

Nothing from Valve is redistributed — the user downloads the recovery image
from Valve's own page (and accepts Valve's terms there); everything is built
locally on their machine.

> Independent hobby project — **not affiliated with or endorsed by Valve,
> NVIDIA, or Microsoft.**

## Download

Grab `SNGI.exe` from the
[latest release](https://github.com/28allday/win-steam/releases/latest) —
no install, no dependencies (WebView2 ships with Windows 10/11; the app
installs WSL2 itself if missing). Run it and follow the wizard.

The exe is unsigned, so Windows SmartScreen will warn on first run: click
**More info → Run anyway**.

## How it works

The original build pipeline is deeply Linux-bound (loop devices, btrfs,
overlayfs chroot, DKMS). Instead of pretending to rewrite that for Win32, the
app runs the **unmodified build script inside a private WSL2 Arch Linux
distro** it sets up itself, then raw-writes the result to USB from the Windows
side (Rufus-style: lock + dismount volumes, write `\\.\PhysicalDriveN`).

Wizard steps:

1. **System check** — admin rights, Windows ≥ 10 2004, S Mode detection,
   CPU virtualization enabled in firmware (WMI probe with a plain-English
   "enable VT-x/SVM in the UEFI" warning if not), ~30 GB free, WSL2 present
   (one-click `wsl --install --no-distribution` if not).
2. **Builder setup** — creates a private `steamos-builder` distro from the
   official Arch WSL image (`wsl --install archlinux --name steamos-builder`,
   falling back to a direct mirror download + `--import`), installs the build
   tools, verifies the WSL2 kernel has btrfs/overlayfs/loop support.
3. **Recovery image** — opens Valve's download page; the user drags the
   downloaded `steamdeck-recovery-*.img.bz2` into the app (or browses).
   No extraction needed.
4. **Build** — copies the image into the builder and runs
   `steamos-nvidia-installer.sh` (embedded, byte-identical to upstream except
   one `udevadm || true` guard — no udev daemon under WSL2). Live log
   streaming. 15–30 min.
5. **Flash** — lists USB-bus disks only (never boot/system disks),
   double-click-to-confirm, raw-writes the image read straight from
   `\\wsl.localhost\steamos-builder\build\…`, with progress.
6. **Finished** — target-machine instructions (UEFI, Secure Boot off, RTX
   20-series+).

The builder distro is reusable (cached driver builds) and removable from the
sidebar (~20 GB back).

## Layout

```
main.go                    Wails v2 app shell (webview GUI, file drop enabled)
app.go                     All bound backend logic + event streaming
internal/wsl/              wsl.exe wrapper (UTF-16 output decode, hidden
                           windows, streamed exec, UNC paths)
internal/flash/            USB disk enumeration (PowerShell Get-Disk) and the
                           raw physical-drive writer (volume lock/dismount,
                           sector-aligned writes, diskpart-clean fallback)
scripts/builder-setup.sh   Prepares the Arch distro (runs in WSL as root)
scripts/run-build.sh       Copy/decompress image + invoke the installer script
scripts/steamos-nvidia-installer.sh
                           Embedded copy of the upstream build script
frontend/dist/             Static wizard UI (no npm — Wails auto-injects its
                           runtime; plain HTML/CSS/JS)
build/winres/              App icon + requireAdministrator manifest
steamos-nvidia-installer.sh
                           Pristine upstream reference copy (not embedded)
```

## Building the .exe

Cross-compiles from Linux (or builds on Windows) — Wails v2 Windows targets
are pure-Go (WebView2 via syscalls), no CGO, no npm, no wails CLI:

```bash
./build.sh          # → dist/SNGI.exe (~11 MB)
```

Requires Go 1.22+. The manifest embeds `requireAdministrator`, so the app
always launches elevated (WSL setup + raw disk writes need it). End users need
nothing preinstalled except WebView2 (ships with Windows 10/11) — WSL2 is
installed by the app itself if missing.

## Syncing the build script from upstream

`scripts/steamos-nvidia-installer.sh` must stay in lockstep with the upstream
script. To update:

```bash
cp <upstream>/steamos-nvidia-installer.sh scripts/steamos-nvidia-installer.sh
# 1. no udev daemon under WSL2
sed -i 's|^udevadm control --reload$|udevadm control --reload 2>/dev/null \|\| true  # no udev daemon under WSL2|' \
  scripts/steamos-nvidia-installer.sh
# 2. WSL2 kernel lacks CONFIG_UNICODE → cannot mount SteamOS home (ext4
#    casefold). Valve ships no casefolded dirs, so the flag is inert —
#    clear it and retry when the kernel mount fails.
sed -i 's|^mount "\$HOMEPART" "\$HOMEMNT"$|mount "$HOMEPART" "$HOMEMNT" 2>/dev/null \|\| { log "home mount failed — kernel lacks ext4 casefold support (WSL2); clearing the unused casefold flag"; e2fsck -fy "$HOMEPART" >/dev/null 2>\&1 \|\| true; debugfs -w -R "feature -casefold" "$HOMEPART" >/dev/null 2>\&1; e2fsck -fy "$HOMEPART" >/dev/null 2>\&1 \|\| true; mount "$HOMEPART" "$HOMEMNT"; }|' \
  scripts/steamos-nvidia-installer.sh
# 3. pacman's 10 s download timeout is too tight for WSL2/VPN networking
sed -i 's|^PACOPTS="--noconfirm --needed"$|PACOPTS="--noconfirm --needed --disable-download-timeout"|' \
  scripts/steamos-nvidia-installer.sh
```

(Three single-line replacements, line count unchanged — the script's `--help`
extraction depends on its header line numbers. All three are no-ops on native
Arch: the kernel mount succeeds so the fallback never runs, and the pacman
flag is harmless.)

## Testing status

- [x] Cross-compiles clean from Linux (`go vet` + shellcheck clean)
- [x] Full E2E in a QEMU Windows 11 Home VM (2026-07-19): WSL install path,
      builder setup, image copy, driver build, USB flash, host-side verify —
      and the flashed stick boots to the SteamOS desktop with both installer
      icons (see `vmtest/` + `NOTES.md`)
- [ ] End-to-end on a real Windows box + real USB stick, booted on the RTX rig
- [ ] Drag-and-drop image pick (only Browse exercised in the VM)
- [ ] "Remove builder…" button
- [ ] Flash a disk that Windows holds open (diskpart-clean fallback path)
- [ ] Old-WSL fallback (`--import` of the mirror image)

## Disclaimer

Use at your own risk. SteamOS is Valve's; the NVIDIA driver is NVIDIA's.
Flashing erases the selected USB drive; installing SteamOS erases the selected
target disk.
