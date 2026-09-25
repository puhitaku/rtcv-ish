#!/usr/bin/env bash
# Prints the body of the "## <version>" section of CHANGELOG.md.
# Fails if the section is missing or empty.
set -euo pipefail

version=${1:?usage: $0 <version> [changelog]}
changelog=${2:-CHANGELOG.md}

body=$(awk -v heading="## $version" '
	$0 == heading || index($0, heading " ") == 1 { found = 1; next }
	found && /^## / { exit }
	found { print }
' "$changelog" | sed -e '/./,$!d')

if [[ -z ${body//[[:space:]]/} ]]; then
	echo "error: $changelog has no non-empty section \"## $version\"; add one before tagging" >&2
	exit 1
fi
printf '%s\n' "$body"
