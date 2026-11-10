#!/usr/bin/env bash
# Builds axx's macOS launcher as axx.app (ADR 0016): one universal executable (arm64 and amd64)
# in a bundle whose identifier is us.nimbusxr.axx.launcher, signed with NimbusXR's Developer ID
# under the hardened runtime, then notarized and stapled.
#
#   scripts/macos-launcher-app.sh <output directory> <version>
#
# AXX_SIGN_IDENTITY names the signing identity (its name or its SHA-1). Notarizing takes an App
# Store Connect API key: APPLE_API_KEY_PATH (its .p8 file), APPLE_API_KEY_ID and
# APPLE_API_ISSUER_ID. Without them the app is signed but not notarized.
set -euo pipefail

out=${1:?usage: macos-launcher-app.sh <output directory> <version>}
version=${2:?usage: macos-launcher-app.sh <output directory> <version>}
identity=${AXX_SIGN_IDENTITY:?AXX_SIGN_IDENTITY names the Developer ID to sign with}

cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

app="$out/axx.app"
if [[ -e $app ]]; then
  rm -rf -- "$app"
fi
mkdir -p "$app/Contents/MacOS"

for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags="-s -w" \
    -o "$work/axx-launcher-$arch" ./internal/tools/axx-launcher
done
lipo -create -output "$app/Contents/MacOS/axx-launcher" "$work"/axx-launcher-arm64 "$work"/axx-launcher-amd64
sed "s/@VERSION@/$version/g" internal/tools/axx-launcher/Info.plist >"$app/Contents/Info.plist"
plutil -lint -s "$app/Contents/Info.plist"

codesign --force --options runtime --timestamp --sign "$identity" "$app"
codesign --verify --strict --verbose=2 "$app"

if [[ -n ${APPLE_API_KEY_PATH:-} ]]; then
  ditto -c -k --keepParent "$app" "$work/axx.zip"
  xcrun notarytool submit "$work/axx.zip" --wait \
    --key "$APPLE_API_KEY_PATH" --key-id "${APPLE_API_KEY_ID:?}" --issuer "${APPLE_API_ISSUER_ID:?}"
  xcrun stapler staple "$app"
  spctl --assess --type execute --verbose=2 "$app"
fi
