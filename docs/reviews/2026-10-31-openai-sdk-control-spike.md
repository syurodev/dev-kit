# OpenAI SDK control spike (R3.2)

Date: 2026-10-31
Scope: isolated Go SDK fixture spike in `tools/spikes/openai-sdk`; no production imports or adapter changes.

## Pinned SDK and exact controls

The standalone module pins `github.com/openai/openai-go/v3 v3.91.0` in its own `go.mod` and declares Go 1.26.0. This follows the official [OpenAI Go API Library](https://developers.openai.com/api/reference/go), which documents the v3 SDK and currently recommends pinning `v3.91.0`.

The fixture helper uses `option.WithUnsafeAllowHTTP()` only to permit its local `httptest` HTTP server; it is a test-only transport setting and must not be copied into production client configuration.

The spike exercised these SDK API names:

- `openai.NewClient` with `option.WithAPIKey`, `option.WithBaseURL`, `option.WithHTTPClient`, and `option.WithMaxRetries(0)`. `WithAPIKey` explicitly overrides an ambient `OPENAI_API_KEY` in the fixture. The test sends only the dummy token `dummy-spike-token` to a local `httptest.Server`.
- `option.WithRequestTimeout` and context cancellation on `Responses.New`.
- `option.WithResponseInto(&rawResponse)` to capture the raw `*http.Response` and response headers.
- `Responses.NewStreaming(ctx, responses.ResponseNewParams{...})`; the SDK adds `stream:true` to the request. The fixture also sets `store:false` and a per-request header with `option.WithHeader("OpenAI-Beta", "fixture=v1")`.
- `ResponseReasoningItem.ToParam()` to carry fixture `encrypted_content` into `ResponseNewParams.Input`; `Store:param.NewOpt(false)` and `Include: []ResponseIncludable{ResponseIncludableReasoningEncryptedContent}` serialize as required.
- `ResponseInputItemParamOfAdditionalTools` to serialize an `additional_tools` input item, including the empty-tools form.
- `Models.List(ctx)` to issue `GET /v1/models` with bearer auth.

Source inspection of the pinned SDK shows default retries are configurable by `WithMaxRetries`, and `WithMaxRetries(0)` makes one request in the 500 fixture. The SDK has no content-telemetry endpoint or content upload behavior in the inspected request path. It does send client metadata headers such as `X-Stainless-Lang`, `X-Stainless-Package-Version`, `X-Stainless-OS`, `X-Stainless-Arch`, and runtime version. This spike did not establish any account-side telemetry policy; production privacy review should account for those metadata headers.

CI executes each standalone module separately, since root-module package patterns do not descend into nested modules:

```sh
(cd tools/spikes/openai-sdk && go test ./... && go vet ./...)
```

## Fixture evidence

Run from `tools/spikes/openai-sdk`:

```text
go test -v ./...
PASS

go test -race ./...
ok devkit/tools/spikes/openai-sdk
```

Named test coverage:

- `TestRetriesDisabledAndExplicitBearerRequestPath`
- `TestAPIErrorCapturesStatusCodeCodeRequestIDAndRetryAfter` (401 and 429 fixtures; checks SDK error status/code/type, `x-request-id`, `Retry-After`, and one request)
- `TestExplicitCredentialOverridesEnvironment`
- `TestRawResponseHeaderCapture`
- `TestRequestCancellationAndTimeout`
- `TestStreamingPreservesPartialAndTerminalEvents`
- `TestStreamingFailureAndTruncationAreNotCompletion` (`failed`, `truncated`)
- `TestEncryptedReasoningRoundTripsWithStoreFalse`
- `TestPromptCacheKeyAndAdditionalToolsSerialize` (`empty` and `approved_function` fixtures; empty non-nil tools serialize as `[]`, and the function item round-trips)
- `TestModelsListUsesV1ModelsAndDetectsSIWCShapeMismatch`

The SSE fixtures demonstrate that `response.output_text.delta` remains observable, and that a `response.failed` event or a stream ending without `response.completed` is not success. In these fixtures the SDK parser does not turn `response.failed` into a Go transport error; a caller must inspect the event type and require `response.completed` before accepting a result. The fixture only checks serialization and parser behavior; it does not prove server-side reasoning retention or encryption semantics.

The prompt-cache fixture establishes that the SDK serializes `prompt_cache_key`; it cannot establish a cache hit. SIWC's preview limitations page lists unsupported `prompt_cache_retention` and does not list `prompt_cache_key` as unsupported. Actual SIWC acceptance and cache behavior remain unverified.

## SIWC-specific gap

The generic SDK `Models.List` decoder expects the standard Models response with `data[]` entries such as `{id, created, object, owned_by, shutdown_date}`. The [SIWC Models and inference guide](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference) documents a different account catalog: `models[]` entries filtered by `visibility == "list"`, identified by `slug` (with `display_name`). The fixture confirms `Models.List` requests the correct `/v1/models` path and bearer header but parses the SIWC-shaped `models[]` body as an empty generic page.

Therefore the typed generic method is not sufficient to consume the documented SIWC catalog as-is. An eventual production adapter needs an explicitly selected raw/custom response path or an SIWC-specific decoder. This spike does not choose that implementation or alter the roadmap design.

The SIWC Open Source overview describes dynamic, user-defined agent registration. No OAuth registration, account eligibility, token acquisition, or authenticated inference was attempted. No API key tool was available. All credentials and responses in this spike are dummy fixture values; the evidence is limited to SDK serialization, local HTTP paths/headers, cancellation, timeout, retry, and SSE parser behavior.

## Official documentation reviewed

- [OpenAI Go API Library](https://developers.openai.com/api/reference/go)
- [SIWC Open Source overview](https://developers.openai.com/siwc/token-sharing-open-source)
- [SIWC preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)
- [SIWC models and inference](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [Generic Models list API](https://developers.openai.com/api/reference/resources/models/methods/list)

Design cross-check: `model-adapter.mdx` §11.7 and SDK control §8 were read. This spike flags the SIWC catalog shape mismatch above. No production design or document was changed.
