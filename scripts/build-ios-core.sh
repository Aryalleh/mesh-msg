#!/usr/bin/env bash
# Builds the Go core for iPhone: mobile/ (package meshcore) → ios/Frameworks/Meshcore.xcframework
# Needs macOS with Xcode and Go. Then: cd ios && xcodegen && open Mava.xcodeproj
set -euo pipefail
cd "$(dirname "$0")/.."
GOBIN=$(go env GOPATH)/bin
export PATH="$GOBIN:$PATH"
# gomobile and gobind must match the x/mobile version in go.mod.
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
mkdir -p ios/Frameworks
rm -rf ios/Frameworks/Meshcore.xcframework
gomobile bind -target="${IOS_TARGETS:-ios,iossimulator}" -iosversion 16.0 -ldflags="-s -w" -trimpath \
  -o ios/Frameworks/Meshcore.xcframework ./mobile
ls ios/Frameworks/Meshcore.xcframework
