#!/usr/bin/env bash
set -euo pipefail

base_ref="${1:-main}"
if ! base_sha="$(git rev-parse --verify --end-of-options "${base_ref}^{commit}" 2>/dev/null)"; then
	echo "error: Buf breaking baseline is not a commit: ${base_ref}" >&2
	exit 2
fi

baseline_buf="$(git ls-tree "${base_sha}" -- buf.yaml)"
if [ -n "${baseline_buf}" ]; then
	buf breaking --against ".git#ref=${base_sha}"
else
	echo "compatibility not checked: base ${base_ref} has no Buf module (first schema introduction)"
fi
