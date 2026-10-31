# R3.1: Anthropic Go SDK control spike

**Status:** control surface verified with synthetic HTTP only; no production adapter, provider activation, credentials, or real Anthropic calls.

**Pinned dependency:** `github.com/anthropics/anthropic-sdk-go/v2 v2.3.0`, tag commit `f5c6193d5902070b5e006d5334f5c379cba1f6ae` (released 2026-10-23). The upstream release listing showed v2.3.0 as latest when checked on 2026-10-31. The SDK requires Go 1.25; the isolated spike module uses Go 1.27.1.

Run from the repository root:

```sh
cd tools/spikes/anthropic-sdk-control
go test -v ./...
```

All spike tests pass.

CI executes each standalone module separately, since root-module package patterns do not descend into nested modules:

```sh
(cd tools/spikes/anthropic-sdk-control && go test ./... && go vet ./...)
``` The module and SDK dependency are isolated under `tools/spikes/anthropic-sdk-control/`; the root `go.mod` is unchanged.

## Existing adapter boundary

`internal/agenthub/adapter_claude.go` is the existing Claude **vendor runtime** adapter. It resolves and launches the Claude ACP process under the agent sandbox, then speaks ACP. It does not call Anthropic Messages or reuse an HTTP model client. The native Messages adapter belongs in `internal/harnessruntime/modeladapter` per the accepted package ADR. The existing adapter’s process, sandbox, account-home, and ACP lifecycle are not reusable as Messages API transport or retry logic.

## Verified SDK surface

| Model-adapter requirement | Concrete surface and synthetic evidence | Result / implementation consequence |
| --- | --- | --- |
| One SDK request per kernel attempt | `option.WithMaxRetries(0)` (the SDK default is two retries); `Messages.NewStreaming(ctx, params)`; test `TestSDKRetryDisabledAndRateHeadersRemainAvailable` sees one request after a synthetic 429. | Pass. Keep retries in the kernel and set this option explicitly. |
| Credential comes only from accounts | Construct `anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey(key), ...)`. The first option suppresses SDK environment/profile defaults; explicit `WithAPIKey` emits `Authorization: Bearer …` in v2.3.0. Test sets competing environment credentials/headers and verifies they do not win. | Pass for explicit key injection. The SDK’s default client configuration does load environment/profile values if `WithoutEnvironmentDefaults` is omitted. |
| SDK telemetry disabled | `option.WithoutOpenTelemetry()` disables API-call spans. It is set on the client. | Pass for SDK OpenTelemetry spans/propagation defaults; the native adapter must keep content-bearing telemetry disabled and must not use SDK tool runners. |
| Timeout and cancellation | Pass request `context.Context` to `NewStreaming`; synthetic transport tests verify cancellation and deadline propagate. | Pass. Use an attempt-scoped deadline/context. |
| Raw stream events and provider facts | `Messages.NewStreaming` returns `*ssestream.Stream[MessageStreamEventUnion]`; use `Next`, `Current`, `Err`, `Close`. Union provides `Type`, `AsAny`, `RawJSON`; events preserve synthetic thinking/signature deltas, terminal reason, and input/output/cache usage. `TestRequestOptionsAndRawStreamEventFacts` and `TestSignedThinkingAndToolTurnCanBeSerializedForContinuation` cover request/response serialization. | Pass for known wire events and typed fields. Unknown event types need explicit adapter handling; do not silently discard a nil `AsAny` result. |
| Terminal / partial/error mapping | `TestPartialStreamThenProviderErrorRemainsObservable` sees partial text followed by a typed `*anthropic.Error` with `Type`, `StatusCode`, `RequestID`, `Response`, and `RawJSON`. `TestCleanEOFWithoutTerminalEventNeedsAdapterDetection` demonstrates that clean EOF without `message_stop` returns `Err()==nil`. | Partial errors remain observable. The adapter must track `message_start` and require a recognized terminal event; SDK `Err()==nil` alone cannot mean success. A post-start network cut should map to `Interrupted` when there is no provider terminal error. |
| HTTP errors and headers | For HTTP failures, `*anthropic.Error.Response` exposes status and headers, including `Retry-After`, request ID, and rate-limit headers. The 429 synthetic response confirms this. | Pass for error responses. |
| Successful response headers | Pass `option.WithResponseInto(&rawResponse)` as a request option to `Messages.NewStreaming`. `TestRequestOptionsAndRawStreamEventFacts` verifies response status, `request-id`, and rate-limit headers from the synthetic successful response; the same `*http.Response` remains available after consuming the stream. | Pass. No observer transport is needed. Preserve the response pointer alongside the stream when headers feed `PickAccount`; response headers still require provider-backed validation. |
| Beta headers and request fields | `option.WithHeaderAdd("anthropic-beta", value)` is accepted on the client or per request. Typed `MessageNewParams` supports system blocks, `ThinkingConfigParamOfEnabled`, tool lists, and cache-control markers. Tests inspect serialized request fields. | Pass for setting headers and request JSON; server-side feature availability is not proven here. |
| Provider-hosted tools off | SDK `ToolUnionParam` includes many provider/hosted-tool variants and has no global disable option. `TestProviderHostedToolsRejectedBeforeRequest` exercises a prototype allowlist that accepts only `OfTool` with type `custom`. | SDK alone does not satisfy M-07. Production adapter must construct only approved client-side custom tools, reject all other variants before dispatch, and avoid SDK tool runners/managed-agent helpers. |
| Signed thinking across a tool turn | SDK has `NewThinkingBlock(signature, thinking)` and `NewToolUseBlock` / `NewToolResultBlock`; synthetic serialization test confirms the signature and tool-use/result IDs survive in the outgoing history. | Pass for wire representation only. No valid provider signature was used, so provider acceptance and signature continuity remain unverified. |

## Limits and remaining evidence

All transport tests use `httptest.Server` or a synthetic `RoundTripper`; they prove SDK control and parsing against fixtures, not Anthropic’s live behavior, availability, terms, cache performance, or model support. No credential, real endpoint, or real model call is used.

The §10 provider-specific evidence remains open:

- **Prompt-cache hits through prompt changes:** the SDK serializes cache markers, but this spike cannot measure cache hits across system-prompt patches or tool-set changes.
- **Mid-conversation tool changes:** the SDK serializes a current `tools` list and beta header; whether Anthropic preserves cache for add/remove transitions requires provider-backed evidence and a documented capability decision.
- **Thinking signature acceptance:** the SDK preserves typed/raw signature fields, but only a real response and subsequent real request can establish provider acceptance after a tool result.
- **Successful-response rate limits:** `WithResponseInto` exposes the response headers, but actual header presence and reset semantics still need provider-backed validation.
- **Catalog maintenance:** the SDK provides a Models API client, but this spike does not establish a source of current prices or a versioned capability catalog. Keep price and capability values in the app’s versioned catalog until their authoritative sources and refresh process are decided.

These gaps do not block the finding that v2.3.0 provides the necessary request, retry, context, and raw-stream controls for a Messages adapter. They do block claims that provider cache behavior, signature reuse, live header semantics, catalog freshness, or M-07 enforcement are proven by the SDK alone.

## Test inventory

- `TestRequestOptionsAndRawStreamEventFacts`
- `TestSignedThinkingAndToolTurnCanBeSerializedForContinuation`
- `TestSDKRetryDisabledAndRateHeadersRemainAvailable`
- `TestPartialStreamThenProviderErrorRemainsObservable`
- `TestContextCancellationAndTimeoutStopInFlightRequest`
- `TestCleanEOFWithoutTerminalEventNeedsAdapterDetection`
- `TestTransportFailureIsSurfacedWithoutRetry`
- `TestProviderHostedToolsRejectedBeforeRequest`

## Primary sources checked

- Anthropic SDK [v2.3.0 release](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v2.3.0) and pinned [`option/requestoption.go`](https://github.com/anthropics/anthropic-sdk-go/blob/v2.3.0/option/requestoption.go), [`client.go`](https://github.com/anthropics/anthropic-sdk-go/blob/v2.3.0/client.go), and [`message.go`](https://github.com/anthropics/anthropic-sdk-go/blob/v2.3.0/message.go).
- Anthropic’s [API authentication and headers](https://platform.claude.com/docs/en/api/overview) and [streaming Messages](https://platform.claude.com/docs/en/build-with-claude/streaming) documentation.
