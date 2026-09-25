#!/usr/bin/env bash
# Copies the C++ SDK into the emulator forks.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
src="$root/sdk/cpp/rtcvish"
dst="$root/emulators/melonds/src/frontend/qt_sdl/rtcvish"

rm -rf "$dst"
cp -R "$src" "$dst"
echo "synced $src -> $dst"
