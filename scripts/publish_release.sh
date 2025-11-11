#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required. Install from https://cli.github.com/" >&2
  exit 1
fi

if [ $# -lt 1 ]; then
  echo "Usage: scripts/publish_release.sh <tag> [notes-file]" >&2
  exit 1
fi

TAG="$1"
NOTES="${2:-RELEASE_NOTES.md}"

if [ ! -f "$NOTES" ]; then
  echo "Release notes file '$NOTES' not found" >&2
  exit 1
fi

MAC_APP_PRIMARY="bin/codeswitch.app"
MAC_APP_ALT="bin/CodeSwitch.app"
MAC_ARCHS=("arm64" "amd64")
MAC_ZIPS=()

package_macos_arch() {
  local arch="$1"
  local staging_app="bin/codeswitch-${arch}.app"
  local zip_path="bin/codeswitch-macos-${arch}.zip"

  echo "==> Building macOS ${arch}"
  env ARCH="$arch" wails3 task package ${BUILD_OPTS:-}

  local bundle_path="$MAC_APP_PRIMARY"
  if [ ! -d "$bundle_path" ] && [ -d "$MAC_APP_ALT" ]; then
    bundle_path="$MAC_APP_ALT"
  fi

  if [ ! -d "$bundle_path" ]; then
    echo "Missing asset: $MAC_APP_PRIMARY (or $MAC_APP_ALT)" >&2
    exit 1
  fi

  rm -rf "$staging_app"
  mv "$bundle_path" "$staging_app"

  echo "==> Archiving macOS app bundle (${arch})"
  rm -f "$zip_path"
  ditto -c -k --sequesterRsrc --keepParent "$staging_app" "$zip_path"
  rm -rf "$staging_app"

  MAC_ZIPS+=("$zip_path")
}

perl -0pi -e "s/const\\s+AppVersion\\s*=\\s*\"[^\"]*\"/const AppVersion = \"$TAG\"/" version_service.go

wails3 task common:update:build-assets
for arch in "${MAC_ARCHS[@]}"; do
  package_macos_arch "$arch"
done

env ARCH=amd64 wails3 task windows:package ${BUILD_OPTS:-}

ASSETS=(
  "${MAC_ZIPS[@]}"
  "bin/codeswitch-amd64-installer.exe"
  "bin/codeswitch.exe"
)

for asset in "${ASSETS[@]}"; do
  [ -e "$asset" ] || { echo "Missing asset: $asset" >&2; exit 1; }
  echo "  asset: $asset"
done

# gh release create "$TAG" "${ASSETS[@]}" \
#   --title "$TAG" \
#   --notes-file "$NOTES"
