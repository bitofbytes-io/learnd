#!/bin/sh
# The same checksum-verified standalone CLI is used locally and in CI.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$root/tools/tailwind.env"
case "$(uname -s)-$(uname -m)" in
    Linux-aarch64|Linux-arm64) platform=linux-arm64; checksum=$TAILWIND_LINUX_ARM64_SHA256 ;;
    Linux-x86_64) platform=linux-x64; checksum=$TAILWIND_LINUX_X64_SHA256 ;;
    Darwin-arm64) platform=macos-arm64; checksum=$TAILWIND_MACOS_ARM64_SHA256 ;;
    Darwin-x86_64) platform=macos-x64; checksum=$TAILWIND_MACOS_X64_SHA256 ;;
    *) echo "Unsupported Tailwind platform (requires glibc Linux or macOS)" >&2; exit 1 ;;
esac
verify() {
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$1")
    else
        actual=$(shasum -a 256 "$1")
    fi
    actual=${actual%% *}
    if [ "$actual" != "$checksum" ]; then
        echo "Tailwind checksum mismatch for $platform ($TAILWIND_VERSION)" >&2
        return 1
    fi
}
cache="$root/bin/tools"
binary="$cache/tailwindcss-$TAILWIND_VERSION-$platform"
mkdir -p "$cache"
if [ ! -f "$binary" ]; then
    download=$(mktemp "$cache/.tailwind-download.XXXXXX")
    trap 'rm -f "$download"' EXIT HUP INT TERM
    curl --fail --show-error --silent --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
        "https://github.com/tailwindlabs/tailwindcss/releases/download/$TAILWIND_VERSION/tailwindcss-$platform" \
        --output "$download"
    verify "$download"
    chmod 755 "$download"
    mv "$download" "$binary"
fi
# Recheck cached binaries too; never execute an unverified download or cache entry.
verify "$binary"
exec "$binary" "$@"
