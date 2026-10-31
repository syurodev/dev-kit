# Protobuf workflow

The host API schema is bootstrapped at `proto/fugue/host/v1/host.proto` as a
namespace marker only. It defines no messages, enums, or services; the Hello and
session contract remains for a later design. Generated Go and TypeScript files
are descriptors for that schema, not an application API.

Run `task proto:check` before changing the schema. It installs Buf CLI 1.75.0
and runs lint, format verification, generation, and breaking checks. Individual
steps are available as `task proto:lint`, `task proto:generate`, and
`task proto:breaking BASE_REF=<base-ref>`. Generation writes Go to
`internal/hostapi/gen` and TypeScript to `frontend/src/generated/hostapi`.
`scripts/buf-generate.sh` stages all plugin output before replacing stale
generated files, preserves handwritten Go package documentation, and leaves
existing outputs intact if generation fails. `scripts/test-buf-generate.sh`
verifies failure and successful stale-file replacement with a local fake Buf
executable.

The generator config pins `buf.build/protocolbuffers/go:v1.36.12` and
`buf.build/bufbuild/es:v2.16.0`; runtime dependencies are pinned to
`google.golang.org/protobuf v1.36.12` and `@bufbuild/protobuf 2.16.0`.
The plugin versions are pinned in `buf.gen.yaml`; the optional revision is
omitted, so Buf uses the latest rebuild for each pinned version. CI's generated
file diff check detects output drift. See [Buf generation configuration](https://buf.build/docs/configuration/v2/buf-gen-yaml/)
and [remote plugin versions and revisions](https://buf.build/docs/bsr/remote-plugins/).

For the first schema introduction, a base commit without a root `buf.yaml` is
reported explicitly as “compatibility not checked.” Once the base contains a
Buf module, `buf breaking` runs against that commit and any configuration or
compatibility error fails the check; no later errors are treated as an absent
baseline.
