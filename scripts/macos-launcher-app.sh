#!/usr/bin/env bash
# Builds axx's macOS launcher as axx.app (ADR 0016): one universal executable (arm64 and amd64)
# in a bundle whose identifier is us.nimbusxr.axx.launcher, signed with NimbusXR's Developer ID
# under the hardened runtime, then notarized, and stapled once Apple has notarized it.
#
#   scripts/macos-launcher-app.sh <output directory> <version>
#
# The apps axx tests run under the launcher, and macOS asks for their permissions on its behalf:
# it is signed with the entitlements those need (internal/tools/axx-launcher/entitlements.plist),
# and its Info.plist has the message for each.
#
# AXX_SIGN_IDENTITY names the signing identity: its name or its SHA-1, or "-" to sign ad hoc, as
# the launcher's test does. Notarizing takes an App Store Connect API key: APPLE_API_KEY_PATH (its
# .p8 file), APPLE_API_KEY_ID and APPLE_API_ISSUER_ID. Without them the app is signed but not
# notarized.
#
# AXX_NOTARIZE_TIMEOUT (7m, say) is how long to wait for Apple, for a CI job that must end:
# an app Apple has not notarized by then is left signed, not stapled, and Apple notarizes it all
# the same (scripts/notarized.sh checks). axx downloads the launcher itself, so macOS never
# quarantines it and never needs the ticket stapled to it. Apple's refusal fails the build.
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

codesign --force --options runtime --timestamp \
  --entitlements internal/tools/axx-launcher/entitlements.plist --sign "$identity" "$app"
codesign --verify --strict --verbose=2 "$app"

if [[ -z ${APPLE_API_KEY_PATH:-} ]]; then
  exit 0
fi
key=(--key "$APPLE_API_KEY_PATH" --key-id "${APPLE_API_KEY_ID:?}" --issuer "${APPLE_API_ISSUER_ID:?}")
ditto -c -k --keepParent "$app" "$work/axx.zip"
wait=(--wait)
if [[ -n ${AXX_NOTARIZE_TIMEOUT:-} ]]; then
  wait+=(--timeout "$AXX_NOTARIZE_TIMEOUT")
fi
# notarytool ends with an error when the wait runs out: the submission's status says how it went.
xcrun notarytool submit "$work/axx.zip" "${key[@]}" "${wait[@]}" --output-format json >"$work/submitted.json" || true
id=$(plutil -extract id raw -o - "$work/submitted.json" 2>/dev/null || true)
if [[ -z $id ]]; then
  echo "Apple's notary service took no submission:" >&2
  cat "$work/submitted.json" >&2
  exit 1
fi
status=$(xcrun notarytool info "$id" "${key[@]}" --output-format json | plutil -extract status raw -o - -)
echo "notarization $id: $status"
case $status in
  Accepted)
    xcrun stapler staple "$app"
    spctl --assess --type execute --verbose=2 "$app"
    ;;
  "In Progress")
    echo "Apple is still notarizing $id: axx.app is signed, and not stapled" >&2
    ;;
  *)
    xcrun notarytool log "$id" "${key[@]}" >&2 || true
    exit 1
    ;;
esac
