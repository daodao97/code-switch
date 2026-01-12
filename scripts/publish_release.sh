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
VERSION_VALUE="${TAG#v}"

if [ ! -f "$NOTES" ]; then
  echo "Release notes file '$NOTES' not found" >&2
  exit 1
fi

MAC_APP_PRIMARY="bin/CodeSwitch.app"
CONFIG_FILE="build/config.yml"
MAC_ARCHS=("arm64" "amd64")
MAC_ZIPS=()
LINUX_ASSETS=()

rename_first() {
  local glob="$1"
  local dest="$2"
  shopt -s nullglob
  local matches=($glob)
  if [ ${#matches[@]} -eq 0 ]; then
    echo "Missing artifact for pattern '$glob'" >&2
    exit 1
  fi
  mv "${matches[0]}" "$dest"
}

package_macos_arch() {
  local arch="$1"
  local staging_dir="bin/package-${arch}"
  local staging_app="${staging_dir}/CodeSwitch.app"
  local zip_path="bin/CodeSwitch-macos-${arch}.zip"

  echo "==> Building macOS ${arch}"
  env ARCH="$arch" wails3 task package ${BUILD_OPTS:-}

  local bundle_path="$MAC_APP_PRIMARY"
  if [ ! -d "$bundle_path" ]; then
    echo "Missing asset: $MAC_APP_PRIMARY" >&2
    exit 1
  fi

  rm -rf "$staging_dir"
  mkdir -p "$staging_dir"
  cp -R "$bundle_path" "$staging_app"

  echo "==> Archiving macOS app bundle (${arch})"
  rm -f "$zip_path"
  ditto -c -k --sequesterRsrc --keepParent "$staging_app" "$zip_path"
  rm -rf "$staging_dir"

  MAC_ZIPS+=("$zip_path")
}

package_linux_amd64() {
  echo "==> Building Linux amd64"
  env ARCH=amd64 PRODUCTION="true" wails3 task linux:package ${BUILD_OPTS:-}

  rename_first "bin/*.AppImage" "bin/codeswitch-linux-amd64.AppImage"
  rename_first "bin/*.deb" "bin/codeswitch-linux-amd64.deb"
  rename_first "bin/*.rpm" "bin/codeswitch-linux-amd64.rpm"
  rename_first "bin/*.pkg.tar.*" "bin/codeswitch-linux-amd64.pkg.tar.zst"

  LINUX_ASSETS=(
    "bin/codeswitch-linux-amd64.AppImage"
    "bin/codeswitch-linux-amd64.deb"
    "bin/codeswitch-linux-amd64.rpm"
    "bin/codeswitch-linux-amd64.pkg.tar.zst"
  )
}

perl -0pi -e 's/const\s+AppVersion\s*=\s*"[^"]*"/const AppVersion = "'"$TAG"'"/' version_service.go

yq -i '.info.version = "'"$VERSION_VALUE"'"' "$CONFIG_FILE"
yq -i '.version = "'"$VERSION_VALUE"'"' build/linux/nfpm/nfpm.yaml

wails3 task common:update:build-assets
for arch in "${MAC_ARCHS[@]}"; do
  package_macos_arch "$arch"
done

env ARCH=amd64 wails3 task windows:package ${BUILD_OPTS:-}

if [[ "${SKIP_LINUX:-false}" != "true" && "$(uname -s)" == "Linux" ]]; then
  package_linux_amd64
else
  echo "Skipping Linux packaging (host=$(uname -s), SKIP_LINUX=${SKIP_LINUX:-false})"
fi

ASSETS=(
  "${MAC_ZIPS[@]}"
  "bin/codeswitch-amd64-installer.exe"
  "bin/codeswitch.exe"
  "${LINUX_ASSETS[@]}"
)

for asset in "${ASSETS[@]}"; do
  [ -e "$asset" ] || { echo "Missing asset: $asset" >&2; exit 1; }
  echo "  asset: $asset"
done

# gh release create "$TAG" "${ASSETS[@]}" \
#   --title "$TAG" \
#   --notes-file "$NOTES"
