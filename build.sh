#!/bin/sh
# Builds mw-watch for Windows and macOS into ./dist. No cgo, so all targets build from any OS.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
export CGO_ENABLED=0
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '-s -w -H=windowsgui' -o dist/mw-watch.exe .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '-s -w' -o dist/mw-watch-darwin-arm64 .
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags '-s -w' -o dist/mw-watch-darwin-amd64 .
ls -l dist
