#!/usr/bin/env bash
# Builds the Go core for Android: mobile/ (package meshcore) → android/app/libs/meshcore.aar
#
#   ANDROID_HOME=~/android-sdk scripts/build-android-core.sh
#
# Needs Go, the Android SDK (platform 35) and an NDK under $ANDROID_HOME/ndk.
# ABIS=arm64 builds one ABI (faster); default is arm64, arm, amd64 (emulators).
set -euo pipefail
cd "$(dirname "$0")/.."
: "${ANDROID_HOME:?set ANDROID_HOME to the Android SDK}"
if [[ -z "${ANDROID_NDK_HOME:-}" ]]; then
  ANDROID_NDK_HOME=$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1)
fi
[[ -d "${ANDROID_NDK_HOME:-}" ]] || { echo "no NDK in $ANDROID_HOME/ndk (sdkmanager 'ndk;27.2.12479018')"; exit 1; }
export ANDROID_HOME ANDROID_NDK_HOME

GOBIN=$(go env GOPATH)/bin
export PATH="$GOBIN:$PATH"
# gomobile and gobind must match the x/mobile version in go.mod.
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind

targets=""
for abi in ${ABIS:-arm64 arm amd64}; do targets+="${targets:+,}android/$abi"; done
mkdir -p android/app/libs
gomobile bind -target="$targets" -androidapi 26 -ldflags="-s -w" -trimpath \
  -javapkg=org.meshmsg -o android/app/libs/meshcore.aar ./mobile
ls -la android/app/libs/meshcore.aar
