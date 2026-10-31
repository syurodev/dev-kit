#!/usr/bin/env bash
set -euo pipefail

source_script=$(cd "$(dirname "$0")" && pwd)/buf-generate.sh
test_root=$(mktemp -d "${TMPDIR:-/tmp}/fugue-buf-generate-test.XXXXXX")
trap 'rm -rf "$test_root"' EXIT
mkdir -p "$test_root/bin" "$test_root/internal/hostapi/gen/pkg" "$test_root/frontend/src/generated/hostapi/pkg"
cd "$test_root"
git init -q
printf 'handwritten docs\n' > internal/hostapi/gen/pkg/doc.go
printf 'old go output\n' > internal/hostapi/gen/pkg/old.pb.go
printf 'old ts output\n' > frontend/src/generated/hostapi/pkg/old_pb.ts

cat > bin/buf <<'FAKE_BUF'
#!/usr/bin/env bash
set -euo pipefail
out=''
while (($#)); do
  if [[ "$1" == --output ]]; then out=$2; shift 2; else shift; fi
done
[[ -n "$out" ]] || { echo 'missing --output' >&2; exit 2; }
if [[ "${FAKE_BUF_MODE:-fail}" == fail ]]; then
  exit 17
fi
mkdir -p "$out/internal/hostapi/gen/pkg" "$out/frontend/src/generated/hostapi/pkg"
printf 'new go output\n' > "$out/internal/hostapi/gen/pkg/new.pb.go"
printf 'plugin must not replace authored docs\n' > "$out/internal/hostapi/gen/pkg/doc.go"
printf 'new ts output\n' > "$out/frontend/src/generated/hostapi/pkg/new_pb.ts"
FAKE_BUF
chmod +x bin/buf

before=$(shasum -a 256 internal/hostapi/gen/pkg/doc.go internal/hostapi/gen/pkg/old.pb.go frontend/src/generated/hostapi/pkg/old_pb.ts)
if PATH="$test_root/bin:$PATH" "$source_script"; then
  echo 'expected fake Buf failure' >&2
  exit 1
fi
after=$(shasum -a 256 internal/hostapi/gen/pkg/doc.go internal/hostapi/gen/pkg/old.pb.go frontend/src/generated/hostapi/pkg/old_pb.ts)
[[ "$before" == "$after" ]] || { echo 'generation failure modified existing outputs' >&2; exit 1; }

FAKE_BUF_MODE=success PATH="$test_root/bin:$PATH" "$source_script"
[[ -f internal/hostapi/gen/pkg/doc.go ]]
grep -q 'handwritten docs' internal/hostapi/gen/pkg/doc.go
[[ ! -e internal/hostapi/gen/pkg/old.pb.go ]]
[[ ! -e frontend/src/generated/hostapi/pkg/old_pb.ts ]]
grep -q 'new go output' internal/hostapi/gen/pkg/new.pb.go
grep -q 'new ts output' frontend/src/generated/hostapi/pkg/new_pb.ts
printf 'staged generation preserves outputs on failure and replaces stale outputs on success\n'
