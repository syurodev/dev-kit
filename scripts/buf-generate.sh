#!/usr/bin/env bash
set -euo pipefail

# Stage all plugin output before replacing checked-in artifacts. A remote plugin
# or network failure must leave the current generated files intact.
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"
stage=$(mktemp -d "${TMPDIR:-/tmp}/fugue-buf-generate.XXXXXX")
trap 'rm -rf "$stage"' EXIT

buf generate --output "$stage"

replace_generated() {
  local source_dir=$1 destination_dir=$2 kind=$3
  mkdir -p "$destination_dir"
  if [[ "$kind" == go ]]; then
    find "$destination_dir" -type f -name '*.pb.go' -delete
  else
    find "$destination_dir" -type f \( -name '*_pb.ts' -o -name '*_pb.js' -o -name '*_pb.d.ts' \) -delete
  fi
  if [[ -d "$source_dir" ]]; then
    while IFS= read -r -d '' file; do
      case "$kind:$file" in
        go:*.pb.go|ts:*_pb.ts|ts:*_pb.js|ts:*_pb.d.ts) ;;
        *) continue ;;
      esac
      local relative=${file#"$source_dir"/}
      mkdir -p "$destination_dir/$(dirname "$relative")"
      if [[ "$kind" == ts ]]; then
        # Generator output may end in several blank lines; normalize only the
        # trailing newline run so checked-in generated TS is stable.
        printf '%s\n' "$(<"$file")" > "$destination_dir/$relative"
      else
        cp "$file" "$destination_dir/$relative"
      fi
    done < <(find "$source_dir" -type f -print0)
  fi
}

replace_generated "$stage/internal/hostapi/gen" "internal/hostapi/gen" go
replace_generated "$stage/frontend/src/generated/hostapi" "frontend/src/generated/hostapi" ts
