#!/bin/bash
# make-media.sh — build payload.iso containing SNGI.exe + autounattend.xml.
# Windows setup finds autounattend.xml on any attached media root, and after
# install the same CD provides the app. Re-run after every SNGI rebuild.
set -euo pipefail
cd "$(dirname "$0")"

EXE=../dist/SNGI.exe
[[ -f "$EXE" ]] || { echo "Build first: ../build.sh" >&2; exit 1; }

rm -rf payload payload.iso
mkdir -p payload
cp "$EXE" payload/
cp autounattend.xml payload/
xorriso -as mkisofs -o payload.iso -V SNGI -J -R payload >/dev/null 2>&1
echo "payload.iso ready ($(du -h payload.iso | cut -f1))"
