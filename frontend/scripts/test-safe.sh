#!/usr/bin/env bash
# Runs the frontend tests in sequential chunks, each with a time limit and (where
# systemd-run exists) a memory ceiling, so one runaway test cannot take the whole
# machine down. `bun test --isolate` in a single process has neither.
#
#   bun run test:safe                  # the whole suite, chunk by chunk
#   bun run test:safe src/stores       # only the files under the given paths, one bounded chunk
#
# Environment:
#   TEST_MEM_MAX   memory ceiling per chunk, systemd-run syntax (default 6G)
#   TEST_TIMEOUT   seconds per chunk (default 300)
#
# Exit status is non-zero if any chunk failed, timed out (124) or was killed by
# the memory ceiling (137). The summary names each chunk's status.
set -u

cd "$(dirname "$0")/.."

MEM_MAX="${TEST_MEM_MAX:-6G}"
LIMIT="${TEST_TIMEOUT:-300}"

# GNU timeout is `timeout` on Linux and `gtimeout` from Homebrew coreutils on macOS.
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT=(timeout "$LIMIT")
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT=(gtimeout "$LIMIT")
else
  TIMEOUT=()
  echo "test-safe: no timeout/gtimeout found; chunks run without a time limit" >&2
fi

# A memory ceiling only where systemd can give one. Elsewhere the time limit still applies.
if command -v systemd-run >/dev/null 2>&1 && systemd-run --user --scope -q true >/dev/null 2>&1; then
  BOUND=(systemd-run --user --scope -q -p "MemoryMax=${MEM_MAX}")
else
  BOUND=()
  echo "test-safe: no systemd-run --user scope; chunks run without a memory ceiling" >&2
fi

failed=0
summary=()

run_chunk() {
  local name="$1"
  shift
  echo "== ${name}"
  ${BOUND[@]+"${BOUND[@]}"} ${TIMEOUT[@]+"${TIMEOUT[@]}"} bun test --isolate "$@"
  local rc=$?
  case "$rc" in
    0) summary+=("ok        ${name}") ;;
    124) summary+=("TIMEOUT   ${name} (over ${LIMIT}s)"); failed=1 ;;
    137) summary+=("KILLED    ${name} (over ${MEM_MAX}? exit 137)"); failed=1 ;;
    *) summary+=("FAILED    ${name} (exit ${rc})"); failed=1 ;;
  esac
}

if [ "$#" -gt 0 ]; then
  run_chunk "selected paths" "$@"
else
  # The three directories with the most files get a chunk of their own; every other
  # test file goes in the last one. The file list is written to a temp file and fed to
  # xargs on stdin (no GNU-only `xargs -a`), so it never depends on the shell splitting
  # a long variable.
  run_chunk "src/components" src/components
  run_chunk "src/workbench" src/workbench
  run_chunk "src/modules/agenthub" src/modules/agenthub

  rest="$(mktemp)"
  find src -name '*.test.ts' -o -name '*.test.tsx' \
    | grep -v -E '^src/(components|workbench|modules/agenthub)/' \
    | sort >"$rest"
  if [ -s "$rest" ]; then
    echo "== the other $(wc -l <"$rest" | tr -d ' ') test files"
    ${BOUND[@]+"${BOUND[@]}"} ${TIMEOUT[@]+"${TIMEOUT[@]}"} xargs bun test --isolate <"$rest"
    rc=$?
    case "$rc" in
      0) summary+=("ok        the other test files") ;;
      124) summary+=("TIMEOUT   the other test files (over ${LIMIT}s)"); failed=1 ;;
      137) summary+=("KILLED    the other test files (over ${MEM_MAX}? exit 137)"); failed=1 ;;
      *) summary+=("FAILED    the other test files (exit ${rc})"); failed=1 ;;
    esac
  fi
  rm -f "$rest"
fi

echo
echo "test-safe summary:"
printf '  %s\n' "${summary[@]}"
exit "$failed"
