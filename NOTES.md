# SNGI — status & next steps (updated 2026-07-25, session 4)

## Session 4 — driver version picker (script path E2E-VERIFIED)

The installer script grew `--driver SPEC` (`latest` | `580` | `580.105.08` |
`580.105.08-4`) and the GUI grew a **NVIDIA driver** dropdown on the image
step. Default is unchanged (`latest`), so the previously verified path is
byte-for-byte the same package set.

- Branch list in the GUI is fetched live from
  `archive.archlinux.org/packages/n/nvidia-utils/` (`ListDriverOptions` in
  `app.go`), filtered to ≥575, newest 6, each labelled with its newest
  build. Static fallback only if that fetch fails.
- Script resolves **nvidia-utils first**, then pins `nvidia-open-dkms` +
  `lib32-nvidia-utils` to the same pkgver, then adds `egl-wayland2` **only
  if the chosen nvidia-utils depends on it** (it became a dep at 590 —
  575/580 must NOT get it).
- **Don't "fix" the dep handling to pin every missing dep from Arch.** The
  image genuinely lacks egl-wayland/egl-gbm/egl-x11 (verified: its holo db
  has only glibc + libglvnd of that set) — those come from *Valve's frozen
  mirror* inside the build chroot, and must keep coming from there.
  egl-wayland2 is special because Valve's repo predates it entirely.
- Changing branch with a warm `--workdir` clears the overlay upper +
  ovlwork automatically (cached pacman downloads kept); full recompile.
- `driver.conf` in the image now also records `DRIVER_SPEC`; the self-heal
  repatch already followed `PKG_URLS`, so the choice propagates across OS
  updates for free.

Verified: resolution + download for 575/580/590/595/610/latest and
exact-version specs against the live archive; egl-wayland2 appears only for
590+; shellcheck clean; `go vet` + Windows build clean; the branch scraper
returns 610/595/590/580/575 with correct newest versions.

**FULL BUILD with `--driver 575` PASSED on the Linux desktop (2026-07-25):**
nvidia-open 575.64.05-2 compiled cleanly against the image's
`6.16.12-valve24.4-1-neptune-616` kernel (older branch vs newer kernel was
the real risk — it's fine); payload came out as `egl-gbm egl-wayland egl-x11
eglexternalplatform lib32-nvidia-utils nvidia-utils` with **no**
egl-wayland2; installed `nvidia.ko` reports 575.64.05; `driver.conf` in the
image has `DRIVER_SPEC="575"` + the three archive URLs. Artifacts deleted
after. The GUI dropdown itself has NOT been clicked in the Windows VM yet.

**GUI PATH VERIFIED IN THE WINDOWS VM (2026-07-25):** dropdown populated
live from the archive *from inside Windows* (Latest + 610/595/590/580/575
with versions), picking "575 branch" carried through to
`--driver 575` in WSL — build log showed `Resolving NVIDIA driver packages
from Arch Linux (--driver 575)` → `nvidia-utils 575.64.05-2`,
`nvidia-open-dkms 575.64.05-2`, `lib32-nvidia-utils 575.64.05-1` and **no
egl-wayland2**. Cancelled there (the full compile was already proven
natively). Cancel button still works.

Layout fix that came out of it: the picker pushed **Continue** below the
fold at the 1280x800 minimum window size once a file card was showing.
Trimmed the picker (one-line hint, `grid-column: 2`), dropzone padding
30→22, and filecard/lede margins; re-verified in the VM that everything
fits without scrolling. Watch this when adding anything else to that pane.

Host gotcha from that run: inspecting an image by hand with `losetup -rP`
lets **udisks automount** every partition into `/run/media/gav/` (the script
blocks this with its own udev rule during a real run) — unmount those, and
note btrfs may then hold the loop device until reboot
(`btrfs device scan --forget` says EBUSY).

Upstream `~/Projects/steam break down/steamos-nvidia-installer.sh` carries
the identical change (it's the canonical copy) + README docs. Neither repo
has been committed or pushed for this feature.

# SNGI — session 3 notes (2026-07-19)

**PUBLISHED.** Public repo at <https://github.com/28allday/win-steam>
(28allday GitHub). Release **v0.1.0**
is live with `SNGI.exe` attached; README "Download" section points at
`releases/latest` and covers the SmartScreen warning.

**FULL E2E PASSED IN THE VM** (session 2). Build → flash → host verify →
the flashed usb.img boots in QEMU to the SteamOS recovery desktop with
both custom installer icons. Three product bugs were found and fixed
along the way — all would have hit every real Windows user.

## Cutting a new release (manual, no CI — deliberate choice)

```bash
cd ~/Projects/win_steam
./build.sh                                   # → dist/SNGI.exe + dist/SNGI.zip
git tag v0.1.1 && git push origin master v0.1.1
gh release create v0.1.1 dist/SNGI.exe dist/SNGI.zip --title "SNGI v0.1.1" --notes "..."
```

Attach BOTH assets: the zip is the primary download (browsers sometimes
block bare exe downloads; Defender/SmartScreen behave the same either way,
it's only the download-block it dodges). Put the exe's SHA-256 (build.sh
prints it) in the release notes, and mention the SmartScreen/Defender
false-positive in the Download section — see v0.1.1's notes for wording.
For each release, consider submitting the new exe to Microsoft as a
false positive: <https://www.microsoft.com/en-us/wdsi/filesubmission>
(per-hash, clears in ~1-3 days). Long-term fix = code signing
(SignPath Foundation free-for-OSS, or Azure Trusted Signing).

Identity: 28allday / gavin.nugent@hey.com (desktop global — already right).

## Repo gotchas (learned at publish time)

- `.gitignore` uses **anchored `/dist/`** — a bare `dist/` also swallowed
  `frontend/dist/`, which is hand-written embedded UI SOURCE (no build
  step); ignoring it makes fresh clones fail to compile. Don't "tidy"
  this back.
- `vmtest/appdir/` is ignored (stale exe copy for the VM's app.iso).
- Root `steamos-nvidia-installer.sh` = pristine upstream reference;
  `scripts/steamos-nvidia-installer.sh` = the embedded one with the 3
  sed transforms (see README "Syncing"). Both are committed on purpose.

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

- [x] A build with `--driver 575` end to end — PASSED 2026-07-25 on the
      Linux desktop, and the GUI dropdown → `--driver 575` path PASSED in
      the Windows VM the same day (see session 4 above).
- [ ] Drag-and-drop of the image file (only Browse tested — needs a real
      Explorer drag; hard to fake via QMP)
- [ ] "Remove builder…" button
- [ ] Fresh-VM re-run of the WSL-install flow to see the fixed messages
      (snapshot or reinstall — current VM is past that stage)
- [ ] Real hardware: Windows box + real USB stick + boot it on the RTX rig
- [ ] Code-signing story for SmartScreen (unsigned exe → "Run anyway")
- [x] Git init + first push — DONE 2026-07-19: public 28allday/win-steam,
      v0.1.0 released with exe

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
