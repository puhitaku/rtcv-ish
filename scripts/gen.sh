#!/usr/bin/env bash
# Regenerates all generated code. Generated files are committed.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

bin="$(mktemp -d)"
trap 'rm -rf "$bin"' EXIT

gen_emulator_go() {
	go build -o "$bin/protoc-gen-go" google.golang.org/protobuf/cmd/protoc-gen-go
	protoc \
		--plugin=protoc-gen-go="$bin/protoc-gen-go" \
		--proto_path=api \
		--go_out=. \
		--go_opt=module=github.com/puhitaku/rtcv-ish \
		api/emulator/v1/emulator.proto
}

gen_frontend_go() {
	mkdir -p internal/server/gen
	go tool oapi-codegen -config api/frontend/oapi-codegen.yaml api/frontend/openapi.yaml
}

gen_frontend_ts() {
	(cd web && npx --no-install openapi-typescript ../api/frontend/openapi.yaml -o src/api/schema.d.ts)
}

gen_emulator_go
gen_frontend_go
gen_frontend_ts
