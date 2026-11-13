#!/usr/bin/env bash
# Signs the unsigned release APKs from
# `./gradlew assembleRelease -PversionName=… -PversionCode=…` with your key.
#
#   KEYSTORE=release.jks KEY_ALIAS=mesh-msg KEYSTORE_PASSWORD=… scripts/sign-apks.sh [outdir]
#
# KEY_PASSWORD defaults to KEYSTORE_PASSWORD. Signed copies go to outdir
# (default android/dist) as mesh-msg-<version>-<abi>.apk, each verified.
# The key never leaves your machine / your CI secrets; without one, publish the
# debug APKs instead (they are signed with the public Android debug key).
set -euo pipefail
cd "$(dirname "$0")/.."
: "${KEYSTORE:?path to the keystore}" "${KEY_ALIAS:?key alias}" "${KEYSTORE_PASSWORD:?keystore password}"
OUT=${1:-android/dist}
# Version for the file names: $VERSION, else the latest tag without the leading v.
VERSION=${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}
: "${VERSION:?set VERSION, e.g. 0.2.0-beta1}"
: "${ANDROID_HOME:?set ANDROID_HOME}"
BT=$(ls -d "$ANDROID_HOME"/build-tools/* | sort -V | tail -1)

# apksigner reads passwords from the environment, never the command line.
KEY_PASS_VAR=KEYSTORE_PASSWORD
[[ -n "${KEY_PASSWORD:-}" ]] && KEY_PASS_VAR=KEY_PASSWORD

mkdir -p "$OUT"
trap 'rm -f "$OUT"/*.aligned "$OUT"/*.idsig' EXIT
shopt -s nullglob
apks=(android/app/build/outputs/apk/release/*-release-unsigned.apk)
((${#apks[@]})) || { echo "no unsigned release APKs: run ./gradlew assembleRelease first"; exit 1; }
for apk in "${apks[@]}"; do
  abi=$(basename "$apk" | sed 's/^app-//; s/-release-unsigned\.apk$//')
  out="$OUT/mesh-msg-$VERSION-$abi.apk"
  "$BT/zipalign" -f -p 4 "$apk" "$out.aligned"
  "$BT/apksigner" sign --ks "$KEYSTORE" --ks-key-alias "$KEY_ALIAS" \
    --ks-pass env:KEYSTORE_PASSWORD --key-pass "env:$KEY_PASS_VAR" \
    --out "$out" "$out.aligned"
  rm -f "$out.aligned" "$out.idsig"
  "$BT/apksigner" verify --min-sdk-version 26 "$out"
  echo "signed $out"
done
