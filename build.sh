#!/bin/bash
# build.sh — cross-compile the Windows app from Linux (or build on Windows).
# Output: dist/SNGI.exe
set -euo pipefail
cd "$(dirname "$0")"

echo "[build] Embedding Windows resources (icon, admin manifest)"
go run github.com/tc-hib/go-winres@v0.3.3 make --in build/winres/winres.json --arch amd64

echo "[build] Compiling"
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -tags desktop,production -trimpath \
  -ldflags "-w -s -H windowsgui" \
  -o dist/SNGI.exe .

echo "[build] Done: dist/SNGI.exe"
ls -lh dist/SNGI.exe
