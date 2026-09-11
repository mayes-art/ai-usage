#!/bin/sh
# Run from the repository root inside the official golang:latest Linux image.
set -eu

go version
go test -race ./...
go vet ./...

output=dist/releases
mkdir -p "$output"
go version > "$output/BUILDINFO.txt"
date -u '+Built at %Y-%m-%dT%H:%M:%SZ' >> "$output/BUILDINFO.txt"

for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    target_dir="$output/$target_os-$target_arch"
    mkdir -p "$target_dir"
    binary=aiusage
    if [ "$target_os" = windows ]; then binary=aiusage.exe; fi
    echo "Building $target"
    GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
        go build -trimpath -ldflags='-s -w' -o "$target_dir/$binary" ./cmd/aiusage
    if [ "$target_os" = windows ]; then
        GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
            go build -trimpath -ldflags='-s -w -H windowsgui' -o "$target_dir/aiusage-panel.exe" ./cmd/aiusage
    fi
done

cd "$output"
sha256sum windows-*/*.exe linux-*/aiusage darwin-*/aiusage > SHA256SUMS
echo "Release artifacts and SHA256SUMS are in dist/releases"
