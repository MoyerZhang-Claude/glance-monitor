#!/bin/sh
# Build Glance.exe (Windows x64) from Linux/macOS/Windows with Go >= 1.24.
set -e
cd "$(dirname "$0")"
if command -v llvm-windres >/dev/null 2>&1 || [ -x /usr/lib/llvm-18/bin/llvm-windres ]; then
  WR=$(command -v llvm-windres || echo /usr/lib/llvm-18/bin/llvm-windres)
  (cd res && "$WR" --target=pe-x86-64 -O coff -i app.rc -o ../rsrc_windows_amd64.syso)
fi
go test ./internal/...
GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/Glance.exe .
ls -la dist/Glance.exe
