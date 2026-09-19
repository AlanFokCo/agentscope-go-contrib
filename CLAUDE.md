# Architecture and implementation guide

Use this file to find the code responsible for a behavior and the contracts a
change must preserve. Contribution workflow, community communication, validation
and mandatory evaluator review live in [AGENTS.md](AGENTS.md). API stability and
known hardening limits live in [STABILITY.md](STABILITY.md).

The module is `github.com/agentscope-ai/agentscope-go/v2`. Paths below are relative
to the repository root. Read the implementation and its adjacent tests before
changing an area; this map describes responsibilities, not a guarantee that every
feature is wired into every execution path.

## Source map

| Area | Start here | Responsibility |
|---|---|---|
| Global configuration | [config.go](pkg/agentscope/config.go) | Process configuration and `agentscope.Log()` |
| Messages and events | [message/](pkg/agentscope/message/), [event/](pkg/agentscope/event/) | Typed content blocks and agent lifecycle events |
| Agent interface | [agent.go](pkg/agentscope/agent/agent.go) | `Agent` and embeddable `AgentBase` |
| Agent execution | [unified_agent.go](pkg/agentscope/agent/unified_agent.go) | `UnifiedAgent`, model/tool rounds, middleware and interactive approval |
| Context and recovery | [compress.go](pkg/agentscope/agent/compress.go), [checkpoint.go](pkg/agentscope/agent/checkpoint.go) | Context compression, summaries and checkpoint loading |
| Loop integration | [loop_bridge.go](pkg/agentscope/agent/loop_bridge.go), [loop/](pkg/agentscope/loop/), [runtime/](pkg/agentscope/runtime/) | Agent-to-loop adapters and session execution |
| Model interface and providers | [model.go](pkg/agentscope/model/model.go), [model/](pkg/agentscope/model/) | Chat calls, responses, usage, provider configuration and model cards |
| Message formatting | [formatter/](pkg/agentscope/formatter/) | Provider-specific message and multimodal formats |
| Middleware | [middleware.go](pkg/agentscope/middleware/middleware.go), [middleware/](pkg/agentscope/middleware/) | Lifecycle hooks, budgets, tracing, guardrails and memory integration |
| Tools and permissions | [tool.go](pkg/agentscope/tool/tool.go), [orchestrator.go](pkg/agentscope/tool/orchestrator.go), [permission/](pkg/agentscope/permission/) | Tool contracts, execution and permission decisions |
| Execution backends | [tool/backend.go](pkg/agentscope/tool/backend.go), [workspace/](pkg/agentscope/workspace/) | Local or remote filesystem/command execution and workspace lifecycle |
| State and sessions | [storage/](pkg/agentscope/storage/), [session/](pkg/agentscope/session/) | Storage interfaces, implementations and session state |
| Service and UI | [app/](pkg/agentscope/app/), [service/](pkg/agentscope/service/), [webui/](pkg/agentscope/webui/), [console/](pkg/agentscope/console/) | Application wiring, HTTP handlers and interaction frontends |
| External integrations | [mcp/](pkg/agentscope/mcp/), [a2a/](pkg/agentscope/a2a/), [channel/](pkg/agentscope/channel/) | Tool servers, remote agents and messaging channels |
| Retrieval and memory | [rag/](pkg/agentscope/rag/), [embedding/](pkg/agentscope/embedding/), [memory/](pkg/agentscope/memory/) | Retrieval, embeddings and memory stores |
| Composition and scheduling | [pipeline/](pkg/agentscope/pipeline/), [team/](pkg/agentscope/team/), [schedule/](pkg/agentscope/schedule/) | Agent coordination and scheduled execution |
| Discovery and skills | [credential/](pkg/agentscope/credential/), [hub/](pkg/agentscope/hub/), [skill/](pkg/agentscope/skill/) | Provider configuration, integration discovery and skill loading |
| Edge and device support | [device/](pkg/agentscope/device/), [messagebus/](pkg/agentscope/messagebus/), [connectivity.go](pkg/agentscope/model/connectivity.go) | Device tools, pub/sub and cloud/local routing |
| Observability and evaluation | [tracing/](pkg/agentscope/tracing/), [audit/](pkg/agentscope/audit/), [replay/](pkg/agentscope/replay/) | Traces, audit records, deterministic replay and evaluation |
| Shared internal helpers | [internal/](pkg/agentscope/internal/) | HTTP transport, HTTP server hardening and atomic file replacement |
| Runnable examples | [examples/](examples/) | Usage examples; inspect each example's configuration and service requirements |

## Execution paths and contracts

### Agents, events and middleware

`UnifiedAgent` is the primary agent implementation. `ReActAgent` is the deprecated
JSON tool-calling implementation; avoid extending it when the feature belongs in
the current agent. `UserAgent` and `A2AAgent` serve interactive-input and remote
agent use cases.

`UnifiedAgent.ReplyStream` exposes an event stream, but its `callModel` method
currently calls `ChatModel.Chat`. An event-streaming interface does not imply
streamed token generation from the provider. `UnifiedAgentRunner.LoopOptions`
connects the agent to `loop.Loop`; its model adapter also uses `callModel`.
When changing retries, cancellation, permissions or accounting, trace the
specific entry point and its adapters rather than assuming they share all hooks.

`Middleware` provides reply, reasoning, model-call, acting, system-prompt,
compression and permission hooks, plus tool registration. `BaseMiddleware`
provides defaults. System-prompt transformation is a pipeline; the other hooks
have their own handler contracts. In particular:

- `OnReply` may consume a non-interrupted `ReplyEndEvent` to request another
  round. Interrupted ends must propagate. Forward `CustomEvent` values whose
  names start with `agentscope.`; these carry internal round-boundary signals.
- `OnCheckPermission` and its chain builder exist, but the current unified-agent
  and loop bridge paths do not automatically invoke that middleware hook.
  Follow the actual permission-engine calls when changing enforcement.
- The loop bridge cannot carry interactive approval or external-tool events;
  use `UnifiedAgent.ReplyStream` for that workflow. Do not silently approve a
  tool because an execution path cannot request confirmation.

Preserve event ordering, terminal events and cancellation-aware sends. Check
checkpoint schema compatibility and pending-tool state when changing recovery.
Read [checkpoint tests](pkg/agentscope/agent/checkpoint_test.go) and
[loop bridge tests](pkg/agentscope/agent/loop_bridge_test.go) for those boundaries.

### Messages, formatting and model calls

`message.Msg` carries typed blocks, including text, thinking, tool calls/results,
data and hints. Preserve provider metadata when transforming blocks. Tool results
can contain structured or multimodal content; use their accessors or checked type
switches instead of assuming `Output` is a string. Inspect the relevant formatter
for the supported media types and wire representation.

`model.ChatModel` defines `Chat`, `ChatStream` and `CountTokens`. Provider adapters
live in `model/`; OpenAI Chat Completions and Responses have separate
implementations. Shared types or options do not prove that every provider
serializes a field or interprets it identically. Trace request construction and
response parsing in both call paths.

For provider work:

- Check the exact endpoint, authentication requirements, JSON field names,
  omission rules and option precedence. Use captured HTTP requests in tests;
  checking a Go options struct alone cannot establish the wire format.
- Keep provider-reported `ChatUsage` separate from `CountTokens` estimates.
  Preserve input/output and cache token fields, including in final stream
  responses, wrappers and budgets.
- On successful completion, `ChatStream` emits deltas followed by a final
  assembled response with `IsLast=true`. Consumers must inspect
  `ChatResponse.Error` on every response and handle channel closure and
  `ctx.Err()`: cancellation can close the channel without a final response.
  Do not concatenate the assembled final content onto already accumulated
  deltas. Producers close their output channel and make sends cancellation-aware.
- Test setup failures, partial streams, terminal errors and cancellation.
  `FallbackChatModel` falls back on stream setup errors; it does not restart an
  already-started stream after a terminal error.
- Inspect transport behavior before adding retries. The JSON helper in
  [internal/httpx](pkg/agentscope/internal/httpx/) retries eligible failures with
  backoff and `Retry-After` handling; SSE and provider-specific request paths
  need separate inspection. Do not multiply attempts or duplicate transport logs
  without an explicit reason and tests.

HTTP client defaults and override precedence live in
[model/http.go](pkg/agentscope/model/http.go); individual constructors can also
supply their own defaults. A non-nil custom client is used unchanged. In the
shared client builder, a positive `ClientOptions.Timeout` overrides the default;
zero does not disable it. The shared SSE helper removes the client's total
request timeout and relies on the caller's context. Do not assume this describes
every provider-specific transport. Tests for longer calls must cover cancellation
as well as successful completion.

`SecretStr` and `ResolveAPIKey` support credential handling in provider configs.
Keep keys out of logs, errors and test fixtures committed to the repository.
`GenerateStructuredOutput` uses tool calling; its fallback and retry behavior
must be considered when accounting for model calls and usage.

### Context and model capabilities

[agent/compress.go](pkg/agentscope/agent/compress.go) owns compression thresholds,
summary generation and retention of recent context. `ContextConfig.ContextSize`
is an explicit override. Otherwise `model.ResolveContextSize` checks
`ContextSizer`, then `ModelNamer` plus a model card, then the caller's fallback.
These interfaces are optional; do not assume that a provider or wrapper
implements them.

Embedded cards in [model/models/](pkg/agentscope/model/models/) describe model
metadata. They do not configure a server or establish its runtime context limit.
For local servers and fallback chains, check the configured window of every
model that may receive the request. Delegating metadata to the initially selected
model alone cannot protect a smaller model selected after a failed call.

Context changes must preserve valid tool-call/result relationships and account
for system prompts, summaries, tool schemas and multimodal content. Check
compression failure handling and recorded usage, not only the successful summary
path. Model capability data is not automatically enforced by every tool path;
verify the wiring before documenting such a guarantee.

### Tools, permissions and workspace boundaries

Tools implement `tool.Tool`, commonly by embedding `BaseTool`. The toolkit and
`tool.Orchestrator` handle registration, execution and permission decisions.
Adding a tool requires examining schema validation, error behavior, cancellation
and the relevant permission checks; registration alone does not provide them.
Do not weaken existing safety checks to make a new path work.

File and command tools can use a `tool.Backend` from the context.
`workspace.NewToolBackend` adapts a workspace for use with `tool.WithBackend`.
When that backend is present, inspect its path and operation semantics instead
of assuming local OS behavior. Use `path` for workspace-relative paths with
forward slashes and `filepath` for actual host filesystem paths.

Local file-tool path confinement is opt-in through `tool.WithWorkspaceRoot`;
it does not provide process or network isolation. Permission modes, an
orchestrator sandbox policy and a workspace execution backend have different
responsibilities. A configured policy does not by itself enforce all filesystem,
network or resource restrictions. Verify each claimed boundary in the relevant
backend; consult [known hardening limits](STABILITY.md#open-hardening-work).

Shell safety uses the Unix parser and Windows-specific logic. Add platform guards
for tests that require a Unix shell; do not skip portable behavior merely because
Windows takes a different path. Safety changes need adversarial cases and the
fuzz checks in `AGENTS.md`. Preserve audit records for tool execution and policy
decisions when adding or changing execution paths.

### Persistence and internal services

Storage, session state, replay files and tool writes have distinct durability
contracts. [internal/fsutil](pkg/agentscope/internal/fsutil/) provides atomic
replacement, but not every writer uses it. Check the actual writer before
claiming atomic persistence. Review copying and ownership of nested maps, slices
and pointers when state crosses goroutines or is restored from a checkpoint.

[internal/httpsec](pkg/agentscope/internal/httpsec/) contains shared server
hardening helpers. Inspect authentication, body limits, timeouts and shutdown at
the actual handler/server boundary. Reuse existing helpers where appropriate;
the existence of a helper is not proof that an endpoint uses it.

## Go conventions

- Use `context.Context` as the first argument for blocking operations. Preserve
  parent cancellation through goroutines, retries and outbound requests.
- Return errors for operational failures. Preserve documented constructor
  contracts, including the programmer-error panics in `message.NewMsg` and
  `agent.NewUnifiedAgent`; do not add new panic-based APIs casually.
- Prefer small interfaces, existing embeddable defaults and functional options.
  Preserve source compatibility and document meaningful behavior changes.
- Wrap errors with context while preserving `errors.Is`/`errors.As` matching.
  Use the structured errors and sentinels in
  [errors/](pkg/agentscope/errors/); `AgentError.Is` matches by code.
  `model.IsRetryableError` accepts a typed `Retryable=true` and otherwise falls
  back to error-text matching; `false` alone does not prevent a retry. Use
  `AgentMessage` where an error supplies a model-facing explanation.
- Log through `agentscope.Log()`. Choose levels for the caller's operational
  needs and avoid logging the same retry or failure at several layers.
- Make resource ownership clear: who closes a channel, response body or file;
  who cancels a worker; and whether returned state is shared or copied.
- Keep tests deterministic where possible. Inject transports, clocks or fixtures
  at existing seams instead of requiring real API keys, hardware or long waits.

## Test navigation

| Change | Relevant tests or helpers |
|---|---|
| Agent rounds, recovery or compression | Adjacent `agent/*_test.go`, [agenttest/](pkg/agentscope/agenttest/) |
| Lifecycle events | [event/streamcheck/](pkg/agentscope/event/streamcheck/) and agent stream tests |
| Provider requests, usage and streaming | `model/*_test.go`, [providercontract/](pkg/agentscope/providercontract/) |
| Deterministic replay | [replay/](pkg/agentscope/replay/), [agent/golden_test.go](pkg/agentscope/agent/golden_test.go) |
| Task-level evaluation | [replay/evalkit/](pkg/agentscope/replay/evalkit/) |
| Tool safety or backend routing | Adjacent `tool/*_test.go`, `workspace/*_test.go` and parser fuzz targets |

`providercontract` is test support and imports `testing`; do not add it to
production dependency paths. Inspect its registered harnesses before claiming a
provider is covered. Golden tapes can be regenerated with
`go test ./pkg/agentscope/agent -golden-update` after an intentional behavior
change; review the resulting diff, rather than accepting a new golden file as
proof of correctness.

Use the commands and coverage policy in `AGENTS.md` for final validation. Do not
copy package counts, coverage snapshots or release inventories into this map;
link to the relevant source or measure them when needed.
