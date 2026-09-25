#!/usr/bin/env bash
# Regenerates the nanopb sources of the C++ SDK from the emulator proto.
#
# Needs protoc and the nanopb generator: either `nanopb_generator` on PATH
# (pip install nanopb) or NANOPB_GENERATOR pointing at nanopb_generator.py
# from a nanopb source tree (which needs the protobuf Python package).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/sdk/cpp/rtcvish"
gen="${NANOPB_GENERATOR:-$(command -v nanopb_generator || true)}"

if [[ -z "$gen" ]]; then
    echo "nanopb generator not found; pip install nanopb or set NANOPB_GENERATOR" >&2
    exit 1
fi

cd "$root/api/emulator/v1"
"$gen" -I . -D "$out" -f "$out/emulator.options" emulator.proto
