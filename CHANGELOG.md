# Changelog

Please update changelog as part of any significant pull request. Place short
description of your change into "Unreleased" section. As part of release
process content of "Unreleased" section content will generate release notes for
the release.

## Unreleased

* [frontend] The browser's flagd connection now actually works through
  the frontend's /flagservice proxy. Two proxy bugs starved the
  provider's EventStream so `FlagdWebProvider` never became ready: the
  backend's hop-by-hop headers (notably `transfer-encoding`) were
  forwarded verbatim while Node re-framed the body, and Next.js's
  compression middleware gzip-buffered the stream (JSON-flavored connect
  responses are "compressible"), holding back every event until the
  stream closed. The proxy now strips hop-by-hop headers both
  directions, requests identity encoding from backends, and marks
  responses `Cache-Control: no-transform` so nothing re-encodes them.
  Verified end-to-end by running the real `FlagdWebProvider` (0.7.4,
  latest — already fully compatible with flagd, no library updates
  needed) against a real flagd v0.16.0 binary through the running
  frontend: provider ready, flags resolve with targeting. Compose's
  flagd image is aligned from v0.14.2 to the chart's v0.16.0.

* [image-provider] `/images/...` is now the canonical path end-to-end:
  nginx serves it natively (`location /images/ { alias /static/; }`,
  nothing at the bare root) instead of expecting every proxy and gateway
  to strip the prefix before forwarding — the mismatch that forced
  URLRewrite workarounds in downstream HTTPRoutes. The frontend's
  `/images` proxy, the compose frontend-proxy route, and the chart's
  example HTTPRoute all forward the path unrewritten now; deployments
  can drop their `/images` rewrite filters.

* [chart] The collector agent is now explicitly mandatory: disabling
  both `opentelemetry-collector` and `otelCollectorOperatorCR` fails at
  template time with a clear message instead of deploying an app whose
  every component exports into the void (each one targets
  `OTEL_COLLECTOR_HOST`, and the frontend proxies the browser's
  `/otlp-http` to it). With a collector always rendering, the
  `otlp/observability-backend` endpoint requirement in
  `values.schema.json` stays unconditional — 0.12.11 briefly waived it
  for a fully-disabled collector, a configuration that was never
  actually viable and is now rejected outright.

* [frontend/chart] The app now works fully — product images, feature
  flags, and browser trace export — the moment the frontend is reachable,
  with zero routing wiring. The frontend serves its browser-facing
  same-origin paths itself via streaming proxy API routes (static Next.js
  rewrites `/otlp-http`, `/flagservice`, `/images` → runtime proxies to
  `OTEL_COLLECTOR_HOST`, `FLAGD_HOST`, `IMAGE_PROVIDER_HOST`), replacing
  the removed frontend-proxy hop that k8s deployments previously had to
  reproduce with per-path HTTPRoute rules. A gateway may still route
  those paths straight to the backends as a data-path optimization —
  `otelCollectorHTTPRoute` remains for exactly that — and the chart's
  template-time guard requiring it alongside a published frontend is
  gone, since there is no longer a broken state to guard against.

* [chart] The collector HTTPRoute's paths are now the chart's
  responsibility instead of the consumer's: enabling
  `otelCollectorHTTPRoute` without `rules` previously rendered a route
  with no paths at all, silently dropping the browser trace export it
  exists to serve. Rules now default to the canonical
  `/otlp-http` → collector:4318 (prefix stripped) wiring, and
  `parentRefs`/`hostnames` are inherited from the frontend's httpRoute
  when that is enabled — so `enabled: true` is a complete configuration
  alongside a published frontend. Enabling the route with no parentRefs
  and nothing to inherit fails at template time instead of emitting a
  pathless route.

* [chart] `otelCollectorHTTPRoute` could only target the sub-chart's own
  in-namespace `otel-collector` Service, making it useless for
  bring-your-own-collector deployments (sub-chart disabled, collector in
  another namespace) — exactly where the browser's same-origin
  `/otlp-http/v1/traces` export still needs a route. New
  `otelCollectorHTTPRoute.backendRef.name`/`.namespace` overrides point the
  route at an external collector Service, and a cross-namespace backend also
  renders the `ReferenceGrant` Gateway API requires in that namespace
  (`referenceGrant.enabled: false` opts out).

* [frontend/chart] Browser trace export was posting spans to
  `http://localhost:4318/v1/traces` on every visitor's own machine: the
  chart's default `PUBLIC_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` was an
  absolute localhost URL that real deployments had to remember to
  override per hostname. The endpoint now defaults to the relative path
  `/otlp-http/v1/traces` (app fallback, compose `.env`, and chart), which
  the browser resolves against whatever origin served the page. Since the
  chart routes nothing implicitly, publishing the frontend's HTTPRoute
  without `otelCollectorHTTPRoute` (and without an absolute-URL override)
  now fails at template time instead of silently dropping browser traces.
  Also fixed `_document.tsx` injecting the literal string `'undefined'`
  as the endpoint when the env var is unset.

* [shipping] Live-pod review of the email fixes above turned up an
  unrelated, genuine bug: checkout's `PlaceOrder` was failing continuously
  (dozens of times an hour in this environment) with `shipping quote
  failure: ... expected 200, got 400`. Root cause: `shipping_types.rs`'s
  `Address.state` had no default, but the Go side's protobuf-generated
  struct tags `omitempty` the `State` field when blank - and most
  countries have no state/province concept, so the `state` key is simply
  absent from the JSON for any international address, not present as an
  empty string. Confirmed with a standalone deserialization test against
  the exact JSON shape checkout sends: the original struct fails with
  `missing field 'state'`, the fixed one (`#[serde(default)]`) succeeds.

* [checkout] Same investigation: `quoteShipping` and `shipOrder`'s
  non-200-response error branches both said `"failed POST to email
  service"` - copy-pasted from the actual email-sending code elsewhere in
  this file - when they're POSTing to the shipping service. Harmless but
  actively misleading anyone reading these logs. Fixed both messages to
  say "shipping service".

* [email] Fixed a second, independent bug the previous ConfigurationError
  fix exposed: every `/send_order_confirmation` request started returning
  500, and the error handler's own response also 500'd. Root cause:
  `LoggerProvider#on_emit`'s last statement is
  `@log_record_processors.each { ... }`, and `Array#each` returns its
  receiver - so `on_emit` leaks the processor list as its return value.
  Both `log_warn_or_error` (used by the `error do` handler and the flagd
  retry loop) and the confirmation route ended with a call to `on_emit`
  (directly or via `send_email`) as their last statement, so Sinatra used
  that leaked array as the response body, and `Response#finish` crashed
  calling `.bytesize` on a `LogRecordProcessor` while computing
  Content-Length. Confirmed via a monkeypatch on the exact method to
  capture the real caller, against the real file and a live Sinatra
  request cycle - the crash only reproduces through an actual HTTP
  request, not a direct method call, because it's the Rack response body
  construction that trips over the leaked value. Added an explicit
  `status 200` to the route (matching `/healthz`'s existing pattern) and
  an explicit `nil` return to `log_warn_or_error`.

* [email] Root-caused the `OpenTelemetry::SDK::ConfigurationError` seen on
  every pod start. Reproduced against the exact locked gem versions: the
  service fetched `$logger` from the global proxy before calling
  `OpenTelemetry::SDK.configure`, which creates a proxy logger that gets
  replayed against the real provider once configure runs -
  `opentelemetry-logs-api`'s `ProxyLoggerProvider#delegate=` replays that
  call with positional args, but `opentelemetry-logs-sdk`'s
  `LoggerProvider#logger` has always been keyword-only - a real,
  currently-unfixed mismatch between those two gems (confirmed still
  present as of their latest released versions, 0.4.1 and 0.6.1). Worse
  than just losing logs: because `Configurator#configure` runs
  `logs_configuration_hook` before `install_instrumentation`, the
  swallowed exception also skipped Sinatra's auto-instrumentation
  entirely - no automatic request spans either. Reordered so
  `OpenTelemetry::SDK.configure` runs first, before any logger is
  requested from the global proxy, sidestepping the bug entirely.
  Verified against the real file, not just a reduced repro.

* [email] Live-pod console review turned up a real, continuous bug: checkout
  logged a `WARN "failed to send order confirmation" ... connection refused`
  every single attempt, ~13 times in a 10-minute window. Root cause -
  `email_server.rb`'s health-check port was already fixed to bind the IPv6
  wildcard (`"::"`) for this dual-stack cluster, but the main Sinatra app
  (port 8080, the actual `/send_order_confirmation` endpoint) was left on
  Sinatra's default `0.0.0.0` (IPv4-only), confirmed by Puma's own startup
  line. checkout resolves `email` to an IPv6 address and gets refused every
  time. Added `set :bind, "::"`, matching the health port's existing fix.

* [product-catalog] Same review: `main.go`'s database-ready path logged
  `"Database connection established"` through both `bootLogger.Info(...)`
  (an always-visible, unfiltered handler meant only for startup diagnostics
  before the real logging pipeline exists) and `logger.Info(...)` (the real,
  `LOG_LEVEL`-respecting one) - the `bootLogger` call is why this INFO line
  showed up on console despite `LOG_LEVEL=WARN`. Dropped the redundant
  `bootLogger` call, keeping only `logger.Info`.

* [chart] `opentelemetry-collector.config.exporters."otlp/observability-backend".endpoint`
  no longer has a placeholder default (`otel-gateway:4317`, which pointed
  nowhere real) - it's now enforced as required by `values.schema.json`, so
  `helm lint`/`template`/`install` fail fast with a clear error instead of
  silently deploying a collector that exports telemetry into the void. See
  UPGRADING.md.

* [chart] Added an alternative way to deploy the collector: setting
  `otelCollectorOperatorCR.enabled: true` renders an `OpenTelemetryCollector`
  custom resource (`templates/opentelemetrycollector-cr.yaml`) for the
  OpenTelemetry Operator to reconcile, instead of relying on the
  `opentelemetry-collector` sub-chart's own Deployment/DaemonSet. It reuses
  the sub-chart's `config`/`image`/`mode`/`resources` values, so pipeline and
  exporter overrides work the same regardless of which mode is active. Opt-in
  and mutually exclusive with `opentelemetry-collector.enabled` (the chart
  fails fast if both are set); see
  examples/operator-managed-collector.

* [frontend] Reviewed the browser client's OpenTelemetry setup (auto
  instrumentations, W3C trace-context/baggage propagation, OTLP/HTTP export
  routed same-origin through the proxy to avoid CORS) - implementation was
  already correct, it just had no documentation. Added a "Browser telemetry"
  section to `src/frontend/README.md` describing how it's wired.

* [accounting] Fixed the CI build failure blocking this and every prior
  release: `Helpers.cs`'s `OutputInOrder` called `logger.LogInformation`
  directly with interpolated arguments, which the `CA1873` analyzer flags
  as a build error (expensive argument evaluation when logging is
  disabled). Added a `StartupEnvVar` `[LoggerMessage]` entry, matching the
  rest of this file's pattern, and switched the call site to use it.

* [cart] Fixed the CI build failure blocking this and every prior release:
  `Log.cs`'s `ParseLogLevel(string? value)` used nullable annotations
  (`CS8632`) in a file with no `#nullable` context - unlike accounting,
  cart's `.csproj` has no project-wide `<Nullable>enable</Nullable>`.
  Added a file-scoped `#nullable enable` rather than flipping the whole
  project, to keep the blast radius to this one file.

* [currency] Fixed the CI build failure blocking this and every prior
  release: `logger_common.h`'s `initLogger()` built each `LogRecordProcessor`
  list via `std::vector<std::unique_ptr<T>>{std::move(x)}` - a brace-init
  list, whose backing array is `const T[]`, forces a copy-construct from
  each element, which fails outright for `LogRecordProcessor` (explicitly
  non-copyable and non-movable, per the OTel C++ SDK). Confirmed via a
  standalone repro against the real SDK headers that this exact pattern
  fails with the same "deleted copy constructor" error seen in CI, and
  that building the vector via `push_back` then moving it compiles clean.
  Switched both processor lists (OTLP and console) to that pattern.

* [accounting] `Log.cs`'s `OrderReceivedMessage` template used
  `{@OrderResult}` - Serilog's destructuring syntax, which the
  `[LoggerMessage]` source generator doesn't support; it's just a
  literal placeholder name that doesn't match the `orderResult`
  parameter, and since `OrderResult.ToString()` already emits JSON, the
  field ended up as an unrelated placeholder wrapping already-serialized
  JSON as a string. Fixed the placeholder to `{orderResult}`, matching
  every other entry's naming convention in this file.

* [flagd-ui] This fork's flagd-ui is Elixir/Phoenix, not Node - the
  logger level was hardcoded to `:info` in `config/prod.exs` with no
  runtime override at all. Added `LOG_LEVEL` handling in
  `config/runtime.exs` (debug/warn/error, default info) that sets
  `config :logger, level: ...` at boot.

* [frontend] `utils/Log.ts` unconditionally called `console.warn`/
  `console.error` with no level control at all. Added a `LOG_LEVEL` env
  var (default `warn`, matching current behavior) wired through
  `next.config.js`'s `env` block so it's available in both server and
  client bundles.

* [mcp] Added `LOG_LEVEL` env var support; also removed a duplicate
  `logging.basicConfig(level=logging.INFO)` in
  `astronomy_shop_mcp_server.py` that ran at import time, before
  `run.py`'s own call - since `basicConfig` no-ops once the root logger
  already has a handler, the duplicate silently made `run.py`'s
  configuration (and thus `LOG_LEVEL`) dead code.

* [chatbot] Added `LOG_LEVEL` env var support (previously hardcoded to
  `INFO`), same reasoning as `agent`: no OTel log exporter exists yet, so
  the default stays `INFO`.

* [agent] Added `LOG_LEVEL` env var support (previously hardcoded to
  `INFO`). No OTel log exporter exists in this service yet, so the
  default stays `INFO` rather than the `WARN` default used elsewhere in
  this repo, to avoid silently losing visibility with nowhere else for
  those records to go.

* [chatbot] Same audit: `chat_interface.py`'s catch-all logged only the
  exception's message (no stack trace, via `logging.error(f"Error : {e}")`)
  and returned the raw exception text directly into the chat bubble shown
  to the end user. Now logs via `logging.exception` and returns a generic
  apology message instead.

* [agent] Same audit as the `tools.py` fix below: `agents.py`'s `run_agent`
  turned an unexpected failure inside `agent.ainvoke(...)` into
  `HTTPException(detail=str(e))` with no server-side log at all, so the
  only record of the failure was the raw internal exception text forwarded
  to `chatbot` (and from there into the chat UI). Now logs via
  `logging.exception` and returns a generic detail message. Also fixed
  `mcp_client.py`'s retry/give-up/cleanup logging, which interpolated `{e}`
  into an f-string instead of preserving the exception object - switched
  to `exc_info=True` so stack traces actually survive in the logs.

* [shared] Extending this release's "all helm-deployed components" logging
  review to the newer `agent`/`chatbot`/`mcp` services turned up the same
  leak pattern seen elsewhere, at larger scale: `tools.py` (built into both
  `agent` and `mcp`) has ten tool functions, and every one of them caught
  `Exception`, logged nothing server-side, and returned the raw exception
  string as the tool's own result. A transport failure to cart/checkout/
  shipping/etc. would surface only as raw internal error text (occasionally
  including internal hostnames) that the LLM then relays straight into the
  shopper's chat, with no corresponding log line for on-call to find. All
  ten now log the real exception via `logger.exception(...)` and return a
  generic, safe message instead.

* [shipping] Independent re-verification of this release's earlier shipping
  fixes turned up two small regressions: the `use tracing::error;` import
  added earlier landed out of alphabetical order (a `cargo fmt` violation),
  and the quote-service-call failure was being logged twice at two
  different severities for the same event (`warn!` inside
  `create_quote_from_count`, then `error!` again in the HTTP handler that
  calls it). Fixed the import ordering and removed the redundant inner
  `warn!`, leaving the single log at the HTTP handler boundary that
  actually decides the response.

* [quote] A repo-wide sweep for the "expected outcome marked as an error"
  pattern turned up a real regression in the original fix: `calculateQuote`
  only validated that `numberOfItems` was *present* (`array_key_exists`),
  not that it was numeric - `intval()` silently coerces a string, `null`,
  bool, or array to `0`/`1` with no error, so a malformed (but present)
  value still produced a bogus "successful" quote instead of a 400. Added
  an `is_numeric()` check. Also downgraded the validation-failure log from
  `error` to `warning`: a malformed request is a normal client 400, not a
  system fault.

* [currency] Same sweep, same pattern as the already-fixed product-catalog
  case: `Convert`'s unsupported-currency-code branches called
  `span->SetStatus(StatusCode::kError, ...)` plus `logger->Error`/
  `console_logger->Error` for what's a normal validation outcome of a
  well-formed request (e.g. a stale client-side currency list), not a
  fault - the `INVALID_ARGUMENT` status already returned to the caller
  communicates the outcome. Removed the error-status/log calls, kept the
  span event, and added a `demo.exchange.supported` attribute.

* [checkout] Same sweep: `PlaceOrder`/`chargeCard` collapsed every payment
  failure - including a plain declined card (`InvalidArgument` from
  payment) - into a generic `codes.Internal` response and the deferred
  error-logging closure's `span.RecordError`/`logger.Error` treatment,
  losing the `InvalidArgument` semantic the caller needs and treating a
  routine decline as a fault. `chargeCard`'s caller now propagates
  payment's actual status code instead of hardcoding `Internal`, and the
  deferred closure skips the error-span/log treatment specifically for
  `codes.InvalidArgument`.

* [payment] Same sweep: `charge()`'s catch-all treated `InvalidCardError`
  (invalid number, unsupported brand, expired card - the single most
  common "expected" outcome in a checkout flow) identically to unexpected
  faults (flagd/network errors), marking the span an error and logging at
  `warn` for every declined card; `index.js`'s gRPC handler did the same at
  the RPC boundary despite already correctly mapping the error to
  `INVALID_ARGUMENT`. Both now branch on `InvalidCardError` specifically:
  a span event/attribute plus an `info`-level log instead of
  `recordException`/`setStatus(Error)`/`warn`.

* [payment] Critical fix caught by the sixth re-review pass (verified
  empirically against the real pino source, not just read): `logger.js` was
  calling `pino(transport, {...})` - options and destination reversed from
  pino's actual `pino(options, destination)` signature. Pino sees a
  stream-like first argument and silently drops the second entirely, so
  `level`, `mixin()`, and `formatters` never took effect. This wasn't
  introduced by this release's earlier payment commit - it's been broken
  since the file was first written, and every previous "fix" to this
  file's level configuration was inert. Consequences: `service.name` was
  missing from every emitted record, the level field rendered as a raw
  number instead of a string label, and `LOG_LEVEL` could never actually
  raise verbosity below pino's default `info` root level. Swapped the
  argument order.

* [product-catalog] User-flagged design issue, not caught by any prior
  audit pass: `GetProduct`'s not-found branch (`sql.ErrNoRows`) was calling
  `span.SetStatus(otelcodes.Error, msg)` - marking the span itself as an
  error for a client looking up a product ID that simply doesn't exist.
  That's an expected outcome of a well-formed request, not a fault; doing
  this pollutes span-based error-rate dashboards/alerting the same way
  logging it at ERROR would pollute log-based alerting (which this file
  already correctly avoids for this exact path). Removed the `SetStatus`
  call; kept the span event and added a `demo.product.found` attribute so
  the outcome stays traceable without being flagged as an error. The gRPC
  `NotFound` status still communicates the result to the caller.

* [frontend] Same design issue, one level up the stack: `ProductCatalog.gateway.ts`'s
  `getProduct` unconditionally called `Log.error(...)` on ANY error from the
  backend, including a legitimate `NOT_FOUND`, and the API route had no
  handling for it either - so a normal "product doesn't exist" navigation
  (mistyped URL, stale link) would log an application error and fall
  through to `InstrumentationMiddleware`'s generic 500 response, instead of
  the client seeing a real 404. The gateway now skips the error log for
  `GrpcStatus.NOT_FOUND`, and `pages/api/products/[productId]/index.ts` now
  catches it specifically and returns a proper `404` before the error can
  reach the middleware's generic error path.

* [frontend] Follow-up from the fifth re-review pass, four fixes: (1)
  `pages/api/healthz.ts` was the one API route not wrapped by
  `InstrumentationMiddleware` - harmless today since the handler body can't
  throw, but a live gap if it's ever extended. (2) `InstrumentationMiddleware`
  force-cast `trace.getSpan(context.active())` to `Span`, so a request with
  no active span would throw a `TypeError` from inside the very catch/finally
  meant to guarantee a safe response; now typed as `Span | undefined` with
  optional chaining throughout, and `runWithSpan` skips the `context.with`
  wrapping entirely when there's no span. (3) Three routes'
  method-not-allowed branches (`currency.ts`, `cart.ts`, `shipping.ts`)
  called `res.status(405)` with no `.send()`/`.json()`/`.end()` -
  `NextApiResponse.status()` only sets the code, it doesn't terminate the
  response, so an unsupported-method request would hang instead of getting
  a 405; all three other routes already correctly call `.send('')`, now
  these three match. (4) The React error boundary in `_app.tsx` was nested
  *inside* all five context providers (`ThemeProvider`, `OpenFeatureProvider`,
  `QueryClientProvider`, `CurrencyProvider`, `CartProvider`), so a render-time
  throw from within any of them would occur above the boundary and go
  uncaught - moved the boundary to wrap everything, including the providers.

* [telemetry-docs] Follow-up from the fifth re-review pass: same invalid
  `access_log on;` fix as image-provider's `/status` location. Also fixed
  the `url.path` log field, which used `$otel_route` (the low-cardinality
  span-naming template, e.g. `/attributes/{business_domain}`) instead of
  `$uri` (the actual request path) - collapsing every `/attributes/*.html`
  or `/services/*.html` request to the same templated string in the access
  log, making the actual page requested unrecoverable. Low cardinality is
  the right call for a span *name*; a log line should keep the real path.

* [image-provider] Follow-up from the fifth re-review pass: the `/status`
  location's pre-existing `access_log on;` directive is not valid nginx
  syntax (only `off` is a recognized keyword; anything else is parsed as a
  file path) - it silently fell back to the default combined text format
  instead of the new `otel_json` format, and would have attempted to write
  to a file literally named `on`. Removed the directive so `/status`
  inherits the correct http-level `access_log` setting.

* [payment] Follow-up from the fifth re-review pass, verified empirically
  against the actual pinned pino/pino-opentelemetry-transport versions: the
  root pino logger's `level: 'info'` is a hard gate in pino - a call below
  it never reaches any transport regardless of that transport's own level.
  This silently defeated `LOG_LEVEL=debug`/`trace` on the console target
  despite the code advertising full env-var control. Set the root level to
  `'trace'` so each target's own `level` does the real filtering.

* [recommendation] Follow-up from the fifth re-review pass: `logger.setLevel(logging.INFO)`
  on the root logger silently capped `LOG_LEVEL=DEBUG` (or any value below
  INFO) on the console handler - a Python logger drops a record before it
  ever reaches a handler if the record is below the *logger's* own level,
  regardless of what the handler's own level allows. Set the logger itself
  to `DEBUG` (permissive) and pinned the OTLP handler explicitly to `INFO`
  (previously `NOTSET`, which only worked because the logger was
  incidentally gating it to INFO+ already) so each handler does its own
  filtering independently, matching the pattern already used correctly in
  other services. Also stopped `ListRecommendations` interpolating the raw
  downstream exception text into the `grpc.StatusCode.UNAVAILABLE` message
  returned to RPC clients - now a generic message, detail stays in the log.

* [email] Follow-up from the fifth re-review pass: `set :logging, false`
  correctly suppressed Sinatra's per-request access log, but Sinatra's
  separate `dump_errors` setting (default `true` outside `:test`) still
  wrote a raw, unformatted, un-leveled backtrace straight to stderr on
  every unhandled exception - bypassing both `$console_logger`'s level gate
  and the OTel logger, in addition to (not instead of) the `error do`
  handler. Added `set :dump_errors, false`. Also made the `LOG_LEVEL`
  parsing defensive: `Logger.const_get(...)` would raise `NameError` and
  crash the process at startup on an invalid value (e.g. a typo or empty
  string) - now falls back to `WARN`.

* [accounting] Follow-up from the fifth re-review pass: the duplicate-order
  catch block (`Consumer.cs`) called `Log.DuplicateOrderSkipped(_logger)`
  with no `Exception` parameter at all - not even `.Message` - so the
  actual constraint-violation exception was never recorded anywhere. Added
  an `Exception` parameter to the generated log method and passed it
  through.

* [fraud-detection] Follow-up from the fifth re-review pass: the console
  `PatternLayout` had no `%p`/`%level` conversion specifier, so an operator
  reading stdout (WARN+ only, per the appender's `ThresholdFilter`) couldn't
  tell WARN from ERROR on any given line - the one piece of information the
  WARN+ gate makes most relevant was the one field missing. Also wrapped
  flagd provider construction/registration in `main()` in a try/catch: it
  ran unprotected before the Kafka consumer loop even started, so a bad
  flagd config would crash the whole service on the same "non-critical
  feature-flag dependency" the rest of the file explicitly isolates (see
  `getFeatureFlagValue`'s existing guard).

* [currency] Follow-up from the fifth re-review pass: the try/catch in both
  `GetSupportedCurrencies` and `Convert` only wrapped the business-logic
  portion of each method - the span-setup/context-extraction preamble
  (`Extract`, `StartSpan`) ran unprotected before the try, so an exception
  there would still crash the process, defeating the whole point of having
  a catch-all boundary. Restructured both so the entire method body is
  covered, with `span` declared outside the try (default-null) so the catch
  blocks can still record onto it if it was created but won't dereference a
  null span if setup itself failed. Also removed an unreachable trailing
  `return Status::OK;` in `Convert` left over from an earlier refactor.

* [product-catalog] Follow-up from the fifth re-review pass: a `fmt.Sprintf`
  was still being fed into `logger.LogAttrs` at the products-loaded log site
  (the exact anti-pattern already swept elsewhere in this file). The
  feature-flag-triggered simulated failure path in `GetProduct` returned an
  `Internal` error without logging it or recording it on the span, unlike
  every other error path in the same function. `bootLogger`'s
  `slog.NewJSONHandler(os.Stdout, nil)` defaults to an `Info` floor despite
  this logger's whole purpose being maximum startup visibility - a future
  `bootLogger.Debug(...)` call would have been silently dropped; now
  explicit `Level: slog.LevelDebug`.

* [checkout] Follow-up from a fifth re-review pass (parallel independent
  agent audits, one per service): `prepOrderItems` dropped the underlying
  error entirely on two paths (`fmt.Errorf("failed to get product #%q", ...)`
  / `"failed to convert price..."`, both missing `%w err`) instead of
  wrapping it, and a `runtime.Start` failure was logged via
  `logger.Error((err.Error()))` - an unstructured message with a stray
  double-parenthesis, missing the `slog.Any("error", err)` field used
  everywhere else. Also removed a dead duplicate
  `saramaConfig.Producer.Return.Successes = true` assignment in
  `kafka/producer.go` (already set two lines earlier).

* [checkout] Follow-up from a fourth re-review pass: `PlaceOrder` - the
  single most important handler in the demo - had six early-return error
  paths (order UUID generation, cart/shipping prep, order totaling ×2, card
  charge, shipping) that returned a gRPC error to the client without a
  single `logger` call; only `span.RecordError` captured them, so a
  `PlaceOrder` failure was visible in traces but invisible in logs. Rather
  than adding six near-duplicate log calls, extended the existing deferred
  closure (which already does `span.RecordError(err)` using the function's
  single shared `err` variable) to also log once, guaranteeing every
  failure path is covered without touching each call site individually.

* [shipping] Follow-up from a third re-review pass: `get_quote`'s error
  branch neither logged the failure nor gated what reached the client - it
  formatted the raw error straight into the 500 response body
  (`format!("Failed to get quote: {}", e)`), the same "caught but not
  logged, and leaked to the client" pattern already fixed in `cart` and
  `quote`. Added `error!(error = %e, ...)` and replaced the response body
  with a generic message.

* [product-catalog] Follow-up from a second re-review pass: `ListProducts`
  and `SearchProducts` only set span status on a DB error, unlike
  `GetProduct` (fixed in the prior commit), which also logs. Added the
  matching `logger.Error` + `span.RecordError` calls to both for
  consistency across all three handlers.

* [shipping] Follow-up from a re-review: `OtelGuard::shutdown` used
  `eprintln!` for provider-shutdown failures instead of `tracing::error!`,
  bypassing the console formatting/level work done elsewhere in this pass.
  Switched to structured `error!(error = %e, ...)`, consistent with the
  rest of the service.

* [quote] Follow-up from a re-review: `index.php` called
  `addErrorMiddleware(true, true, true)` with hardcoded booleans, completely
  ignoring the `displayErrorDetails`/`logError`/`logErrorDetails` values in
  `Settings` (which were `false`/`false` and had no effect either way) and
  never passing a logger - an unhandled `Throwable` never reached Monolog at
  all. Now reads the actual settings and passes the DI-resolved logger. Also
  found and fixed a second stray unstructured `echo`-based access log (the
  same class of bug already fixed in `email`) that printed on every request
  regardless of `LOG_LEVEL` - replaced with a real logger call, including
  the startup "Listening on" banner.

* [product-catalog] Follow-up from a re-review: this service was scoped as
  "already best-in-class, just needs LOG_LEVEL," but the earlier
  exception-handling audit had flagged mixed structured/stringified logging
  throughout `main.go` that never actually got fixed. Swept every remaining
  `logger.Error(fmt.Sprintf(...))`/bare `err.Error()` call to structured
  `slog` calls with the error as an attribute, and removed a redundant
  duplicate log line in the database-init failure path.

* [currency] Follow-up from a re-review: `GetSupportedCurrencies` was still
  the one public gRPC handler with zero exception handling (the original
  audit flagged this; only `Convert` got a guard). Wrapped it in the same
  try/catch pattern as `Convert` so an unexpected exception there returns a
  proper `Status::CANCELLED` instead of propagating uncaught and crashing
  the process.

* [accounting] Follow-up from a re-review of the logging/exception pass:
  `SetErrorHandler` logged fatal Kafka client errors (`error.IsFatal`) but
  never acted on that flag - the consumer kept running indefinitely on a
  client librdkafka itself considers unrecoverable. Now exits on a fatal
  error so the pod restarts.

* [telemetry-docs] Same fix as image-provider: added an `otel_json`
  `log_format` (JSON, trace/span-correlated via `ngx_otel_module`, OTel HTTP
  semantic-convention field names, using the existing `$otel_route`
  low-cardinality mapping for `url.path`) and pointed `access_log` at it,
  replacing the default plain-text combined format. Same ingestion caveat
  as image-provider applies - this is a format fix, not a pipeline-wiring
  fix.

* [image-provider] nginx access logs were using the default combined
  plain-text format with no trace correlation. Added an `otel_json`
  `log_format` (JSON, `escape=json`) carrying OTel HTTP semantic-convention
  field names plus `trace_id`/`span_id` from `ngx_otel_module`'s
  `$otel_trace_id`/`$otel_span_id` variables, and pointed `access_log` at
  it. Note: this makes the access log itself structured and
  trace-correlated on stdout; it does not by itself wire the log into the
  OTel Collector's `logs` pipeline (that pipeline currently only receives
  via `otlp`, with no `filelog` receiver) - format and ingestion are two
  separate changes.

* [frontend] Fixed `InstrumentationMiddleware` (which wraps every Next.js
  API route) only recording the exception on the OTel span and rethrowing
  raw - added a `Log.error` call so failures actually show up in
  application logs, and it now returns a generic `{ error: "Internal
  server error" }` 500 response instead of letting the raw error propagate
  through Next's default handling. Since every API route already goes
  through this middleware, this single change covers `cart`, `checkout`,
  `currency`, `data`, `products`, `recommendations`, and `shipping` rather
  than needing a per-route try/catch. Also added a React error boundary
  (`components/ErrorBoundary`, wrapped around `<Component />` in `_app.tsx`)
  so a render-time throw shows a minimal fallback and gets logged, instead
  of crashing the whole page with no logging at all.

* [currency] Split the single shared `LoggerProvider`/processor-list into
  two independent providers - one OTLP-only, one console-only - since this
  SDK's `LogRecordProcessor` has no per-processor severity filter, making it
  impossible to give one shared provider different minimum levels for
  console vs. OTLP. `logger` (OTLP) receives every call site unconditionally
  (Info+); `console_logger` receives every `Error` call unconditionally
  (matching the WARN+ console floor, since this service has no `Warn` calls)
  and `Info` calls only when `LOG_LEVEL` is `INFO` or `DEBUG` (default:
  console gets Error only). Previously both sinks received identical
  records at identical severity with no differentiation at all.

* [email] Assigned Tier C ("default, unstructured") for this pass: kept the
  stdlib `Logger`, now with a `LOG_LEVEL`-driven level (default `WARN`)
  instead of hardcoded. Fixed several gaps: Sinatra's default
  `Rack::CommonLogger` was logging an access-log line for every request,
  bypassing `$console_logger`'s level entirely - disabled via `set
  :logging, false`. A stray unstructured `puts` on the send-email success
  path bypassed logging altogether - removed. WARN/ERROR events (flagd
  retry, the top-level Sinatra `error` handler) previously went only to
  console with `.message` alone, never to the OTel logger and never with a
  backtrace - a new `log_warn_or_error` helper now emits to both, with the
  backtrace included in the console message and as OTel log attributes.

* [quote] Assigned Tier B ("default-but-structured") for this pass: kept
  Monolog's default `LineFormatter`, but replaced the hardcoded
  `LogLevel::DEBUG` setting (which only happened to produce a WARN-only
  console today because of a special-cased DEBUG→WARNING ternary in
  `dependencies.php`) with a real `LOG_LEVEL` env var (default `WARNING`)
  read directly in `settings.php`, and removed the now-unnecessary ternary.
  The OTel Monolog handler remains fixed at `LogLevel::INFO` independent of
  this setting.

* [shipping] Assigned Tier B ("default-but-structured") for this pass: kept
  `tracing_subscriber`'s default `fmt` layer (not `.json()`) but split its
  filter from the OTel layer's - console is now `LOG_LEVEL`-driven (default
  `warn`) via `EnvFilter::try_from_env`, while the OTel layer stays fixed at
  `info`. Previously both layers used the identical hardcoded `"info"`
  filter, so console got the same verbosity as the OTLP export. Also
  standardized error logging to attach the error as a structured field
  (`error = %err`) instead of string-interpolating it into the message, in
  `main.rs`'s flagd-retry logging and `quote.rs`'s quote-service-call
  retry/failure logging.

* [payment] Already Tier A (pino JSON) - fixed the level split: console and
  OTLP export previously shared a single unset pino level (defaulting to
  `info`) via one combined record processor, so console got the exact same
  INFO-level noise as the OTLP exporter. Split into two pino transport
  targets - OTLP fixed at `info`, console driven by `LOG_LEVEL` (default
  `warn`) - and dropped the transport's redundant second console record
  processor now that `pino/file` handles stdout directly. Also added
  `logger.warn({ err }, ...)` in `charge.js`, which previously only recorded
  the exception on the span - charge failures were invisible in application
  logs. `index.js` now maps a new `InvalidCardError` (thrown for the three
  actual validation failures) to `grpc.status.INVALID_ARGUMENT`, and
  everything else to `INTERNAL`, instead of every failure surfacing to
  clients as the generic `UNKNOWN`.

* [load-generator] Assigned Tier C ("default, unstructured") for this pass:
  the console `StreamHandler` now has its own WARN+ floor, configurable via
  `LOG_LEVEL` (default `WARNING`), instead of sharing the root logger's
  `INFO` level with the OTLP handler. Dropped the `python-json-logger`
  dependency, which was declared but never actually used by this service
  (recommendation now uses it instead, for its Tier A JSON console). Also
  fixed both Playwright browser-task `except` blocks logging via
  `str(e)`/`logging.error` (discarding the traceback) and silently
  swallowing the failure - now `logging.exception` captures the traceback
  and the exception is re-raised so Locust actually records the task as
  failed instead of reporting a false success.

* [recommendation] Assigned Tier A ("modern JSON") for this pass: added
  `python-json-logger` and switched the console handler's formatter to
  `JsonFormatter`, with its WARN+ floor now driven by `LOG_LEVEL` (default
  `WARNING`) instead of hardcoded. Also wrapped `ListRecommendations` (the
  actual RPC handler, which previously had no exception handling at all) in
  a try/except that calls `context.abort` with a proper status
  (`UNAVAILABLE` for a downstream `grpc.RpcError`, `INTERNAL` for anything
  else) and logs via `logger.exception` so the traceback is captured,
  instead of letting failures propagate raw as an opaque `UNKNOWN` status.

* [cart] Assigned Tier C ("default, unstructured") for this pass: kept the
  default plain-text console formatter but added a `LOG_LEVEL`-driven WARN
  floor (default `WARN`), while the overall minimum level stays
  `Information` so the injected .NET auto-instrumentation bridge still
  captures Info+. Also added the missing `ILogger` calls to `CartService`'s
  three `RpcException` catch blocks (`AddItem`/`GetCart`/`EmptyCart`), which
  previously only recorded the exception on the `Activity`/trace - gRPC
  failures were invisible in application logs entirely.

* [accounting] Assigned Tier A ("modern JSON") for this pass: switched the
  console provider to `AddJsonConsole()` with a `LOG_LEVEL`-driven WARN
  floor (`AddFilter<ConsoleLoggerProvider>`), while the overall minimum
  level stays at `Information` so the injected .NET auto-instrumentation's
  log bridge still captures Info+. Replaced the two raw, unstructured
  `Console.WriteLine` startup calls with `ILogger` calls so they go through
  the same JSON formatter/level gate instead of bypassing logging entirely.
  Also fixed the Kafka-connect retry log passing `e.Message` instead of the
  exception object (dropping the stack trace on every transient retry), and
  narrowed `ProcessMessage`'s catch-all `Exception` handler to the specific
  `InvalidProtocolBufferException`/`DbUpdateException` cases it was actually
  meant to handle - any other exception now propagates instead of being
  silently swallowed as a "parsing failure."

* [fraud-detection] Assigned Tier B ("default-but-structured") for this
  pass: kept the existing `PatternLayout` (with trace/span MDC context) but
  added a `ThresholdFilter` so stdout is WARN+ only, and Root level is now
  `${env:LOG_LEVEL:-INFO}` instead of hardcoded. Also fixed two retry-path
  logs passing `${e.message}` instead of the throwable (dropping the stack
  trace), narrowed the protobuf-parse catch from `Exception` to the specific
  `InvalidProtocolBufferException`, and added exception handling to
  `getFeatureFlagValue` (an OpenFeature/flagd evaluation error would
  previously propagate uncaught and crash the whole consumer loop over a
  non-critical flag lookup).

* [ad] Assigned Tier A ("modern JSON") for this pass: swapped the console
  appender's `PatternLayout` for `JsonTemplateLayout` (added the
  `log4j-layout-template-json` dependency) and added a `ThresholdFilter` so
  stdout only emits WARN+ regardless of the Root logger's level. Root level
  is now `${env:LOG_LEVEL:-INFO}` instead of hardcoded, governing what the
  injected Java agent's log4j2 bridge exports as OTLP. Also fixed `getAds`
  logging `e.getStatus()` instead of the throwable (dropping the stack
  trace) and added a catch-all `Exception` boundary so any unexpected
  failure still returns a proper gRPC `Status` via `onError` instead of
  propagating uncaught.

* [checkout] Assigned Tier B ("default-but-structured") for this pass:
  switched the console handler from JSON to `slog.TextHandler` and actually
  applied the WARN floor its own code comment already claimed but never
  wired up (`slog.NewJSONHandler(os.Stdout, nil)` had no `Level` option, so
  it silently emitted everything the OTel handler did). Added a `LOG_LEVEL`
  env var (default `WARN`) controlling that floor. Also swept every
  `fmt.Errorf(...: %+v", err)` in this service to `%w` so `errors.Is`/`
  errors.As` chains actually work, and converted the remaining
  `logger.Error(fmt.Sprintf(...))`/`logger.Warn(fmt.Sprintf(...))`
  call sites to structured `slog` calls with the error/values as attributes
  instead of baked into the message string.

* [logging] Kicking off a repo-wide logging consistency pass: every service
  gets a `LOG_LEVEL` env var, console output stays WARN+ while the injected/
  explicit OTel log export still captures INFO+, and the demo deliberately
  spreads console *format* across three tiers (modern structured JSON,
  framework-default-but-structured, and plain unstructured) instead of
  making every service look the same, so the fleet reflects real-world
  logging heterogeneity. Exception/error-handling idioms are being brought
  in line with each language's best practices as part of the same pass.
  Tracked per-service below as each lands.

* [product-catalog] Added a `LOG_LEVEL` env var (default `WARN`) controlling
  the stdout JSON handler's minimum level, which was previously hardcoded.
  This service was already the reference implementation for the
  console(WARN+)/OTel(INFO+) split introduced here.

* [shipping] Fixed `request_quote` calling `.expect("Invalid quote service
  address")` on every `/get-quote` request instead of only at startup — a
  bad `QUOTE_ADDR` value would panic per-request rather than once at boot
  (in practice the `.expect()` was on a no-op `String -> String` parse, so it
  could never actually fail, but it ran on the request path regardless).
  `QUOTE_ADDR` is now resolved once into a `LazyLock` static at first use.

* [currency] Fixed `Convert` silently producing a wrong (not an error)
  result for an unsupported currency code: `currency_conversion[code]` used
  `operator[]`, which inserts and returns `0.0` for a missing key instead of
  failing, turning an invalid currency code into a division-by-zero/`inf`
  conversion rather than a client error. Now both codes are validated before
  lookup and an unknown code returns `INVALID_ARGUMENT` with a descriptive
  message. Also split the bare `catch(...)` into a `catch(const
  std::exception&)` first so the real error message (`e.what()`) is logged
  and recorded on the span, falling back to the generic message only for
  non-`std::exception` throws.

* [product-catalog] Fixed `mustMapEnv`/startup `net.Listen` failures being
  logged but not fatal, letting the process limp forward broken (e.g.
  `mustMapEnv` left the target env var empty, feeding an invalid `:` address
  into `net.Listen` later). Both now `os.Exit(1)` on failure. Also fixed
  `GetProduct` mapping *every* `getProductFromDB` error (including real DB
  outages, not just a missing row) to gRPC `NotFound` — a database problem
  now correctly distinguishes `sql.ErrNoRows` (still `NotFound`) from any
  other error (now `Internal`, and logged).

* [checkout] Fixed three request-handling bugs in `PlaceOrder`/`main`: (1) a
  failure to empty the cart after a successful order was silently discarded
  (`_ = cs.emptyUserCart(...)`) instead of logged; (2) `net.Listen` failures
  were logged but the server tried to `Serve` on the resulting `nil`
  listener anyway instead of exiting, and a duplicate/unreachable
  `srv.Serve` + graceful-shutdown block after it never actually ran; (3)
  `money.Must(money.Sum(...))` could panic mid-request on a currency
  mismatch with no `recover()` anywhere, taking down the whole process for
  one bad order. Listener failures now exit immediately, the money totaling
  now returns a proper `codes.Internal` error instead of panicking, and the
  cart-empty failure is logged at `WARN` without failing the (already
  successful) order response.

* [cart] Fixed `ValkeyCartStore.AddItemAsync`/`EmptyCartAsync`/`GetCartAsync`
  leaking the full exception (including stack trace, via `$"...{ex}"` string
  interpolation) into the `RpcException` message returned to gRPC clients,
  while never logging it server-side via `ILogger`. Now the exception is
  logged via a new `Log.RedisOperationFailed` and the client only receives a
  generic "Can't access cart storage." message. Also fixed `Ping()` silently
  swallowing Redis errors with no log at all.

* [quote] Fixed `/getquote` silently returning a fake `$0.00` quote with an
  HTTP 200 when the request body was missing `numberOfItems`: the exception
  was only recorded on the OTel span, never logged, and the `finally` block
  returned the default `$quote` value regardless of failure. Now the
  exception is logged via the injected `LoggerInterface` (with the exception
  object in context so Monolog captures the stack trace) and rethrown; the
  route handler catches it and returns a 400 with an error payload instead of
  a bogus successful quote.

* [docker] Fixed a latent `EXPOSE ${VAR}` build failure present in 18
  services' Dockerfiles (accounting, ad, agent, cart, chatbot, checkout,
  currency, email, flagd-ui, frontend, frontend-proxy, image-provider, mcp,
  payment, product-catalog, quote, recommendation, shipping,
  telemetry-docs): the port variables referenced in `EXPOSE` were never
  declared as `ARG`s (or were declared without a default), so builds without
  an explicit `--build-arg` for every port literal failed with `EXPOSE
  requires at least one argument`. Declared each as an `ARG` with the same
  default already used elsewhere in the repo (`.env`, or the service's own
  fallback default in code). Also reordered the dependency-manifest copy/
  restore/fetch steps ahead of full source copies in cart, accounting,
  shipping, and fraud-detection's Dockerfiles so source-only changes reuse
  the cached dependency-resolution layer instead of busting it on every
  build.

* [flagd, recommendation] Found the actual trigger behind the recurring
  `recommendation` call-combiner SIGABRT the previous three releases tried to
  work around: `GRPC_TRACE=call_combiner` debug tracing showed the process
  making ~140 failed gRPC client calls *per second*, continuously, for its
  entire lifetime (50k+ in under 6 minutes) - `filter_stack_call.cc]
  set_final_status CLI UNIMPLEMENTED:Received http2 header with status: 404`.
  This wasn't request traffic (load-generator only runs 2 virtual users); it
  was `openfeature-provider-flagd`'s EventStream, which flagd's own upstream
  issue tracker (open-feature/flagd#1472) documents as reconnecting forever
  with no real backoff whenever the stream fails. `flagd`'s image here had
  been pinned at v0.12.9 since this fork's very first commit and never
  bumped, while upstream open-telemetry/opentelemetry-demo is on v0.16.0 -
  and tellingly, checkout/product-catalog's own Go client dependencies were
  *already* transitively generated against the v0.16.0 schema (flagd's
  EventStream RPC has since been deprecated/replaced), so every provider
  client in this repo already expected the newer generation except the
  server itself. That many failed/retried gRPC calls per second is a far
  more plausible trigger for a rare C-core race than anything else examined
  so far. Bumped flagd v0.12.9 -> v0.16.0 to match upstream exactly, and
  `openfeature-provider-flagd` 0.5.0 -> 0.5.1 in recommendation (also
  matching upstream), which pulled `opentelemetry-api`/`-sdk`/
  `-exporter-otlp-proto-http` 1.42.0 -> 1.43.0 along for a `protobuf`
  constraint (0.5.1 needs >=7.0, 1.42.0's `opentelemetry-proto` capped it
  <7.0). Checked every other provider client in this repo (Java, Go, .NET,
  Node, Ruby, Rust) against upstream's pins for this same flagd version -
  all already match exactly, so no other component needed a version change.
  Also checked flagd v0.16.0's one documented breaking change (disabled
  flags now resolve with `reason=DISABLED` instead of an error) against
  every flag in `demo.flagd.json` and all consuming code - nothing here
  relies on the old behavior. Not yet proven this fully eliminates the
  crash; the previous fixes narrowed real bugs without curing it, so this
  needs a soak test in a live preview environment before calling it closed.
* [recommendation, load-generator, currency] `recommendation` was still
  periodically SIGABRT'ing (`call_combiner.cc:144] Check failed: prev_size >=
  1u`) after the previous two releases' `grpc.enable_retries=0` workarounds.
  The actual root cause: `recommendation`'s log exporter was still the gRPC
  variant (`opentelemetry.exporter.otlp.proto.grpc`), left over from before
  this project moved off OTLP/gRPC for fragility reasons, while its chart env
  points `OTEL_EXPORTER_OTLP_ENDPOINT` at the collector's HTTP port (4318).
  A gRPC client talking to an HTTP listener endlessly retried from the log
  processor's background thread, and that retry churn - not an unfixable
  upstream grpcio bug - is what was tripping the call-combiner assertion.
  Switched `recommendation` to `opentelemetry-exporter-otlp-proto-http`
  instead of re-pointing it at the gRPC port, to stay consistent with that
  policy. Auditing turned up two more components still hardcoded to
  OTLP/gRPC: `load-generator` (same Python gRPC log exporter, same fix) and
  `currency` (C++, all three signals via `OtlpGrpc*ExporterFactory`, plus
  `-DWITH_OTLP_GRPC=ON` at build time) - both switched to their HTTP exporter
  equivalents and moved to port 4318. Left the `grpc.enable_retries=0`
  options in `recommendation` in place as a harmless no-op, but corrected the
  code comments, since they no longer reflect the actual root cause and the
  previously cited grpc/grpc#38251 was an unrelated issue (a different
  check-failure signature entirely). Follow-up in the entry below covers the
  `cart`/`frontend`/`payment`/`product-catalog` question this raised.
* [chart] Components instrumented via the OTel Operator's injected auto-
  instrumentation (`ad`, `fraud-detection`: Java; `cart`, `accounting`:
  .NET; `frontend`, `payment`: Node.js - confirmed per-component by
  inspecting live pods for the operator's `opentelemetry-auto-
  instrumentation-*` init container, plus `accounting`'s total absence of
  any OpenTelemetry package reference in its own `.csproj`) had this chart
  hardcoding their `OTEL_EXPORTER_OTLP_ENDPOINT` anyway. That's a
  deployment-specific value the injected Instrumentation CR already
  supplies, and `cart` proved the duplication isn't just redundant but
  actively wrong: this chart pointed it at the gRPC port (4317) while the
  cluster's actual Instrumentation CR endpoint is HTTP (4318) - the same
  class of silent-drop bug as the `recommendation`/`currency` fixes above,
  just without a crash to surface it. Removed the hardcoded
  `OTEL_EXPORTER_OTLP_ENDPOINT` (and `ad`'s `OTEL_LOGS_EXPORTER`) from all
  six components so the operator's config applies cleanly. By contrast,
  `checkout`, `product-catalog`, and `quote` build their own OTel SDK setup
  in code (Go `otelconf.NewSDK`, PHP `open-telemetry/sdk` +
  `exporter-otlp` with no gRPC transport package) with no injected agent,
  so their explicit endpoints are genuinely required and were left as-is.
* [recommendation] Extended the existing `grpc.enable_retries=0` call-combiner
  workaround (grpc/grpc#26537, grpc/grpc#38251) to the inbound gRPC server,
  not just the outbound product-catalog channel: confirmed in a live PR
  preview environment that the process was periodically SIGABRT'ing
  (`call_combiner.cc:144] Check failed: prev_size >= 1u`) despite the
  existing client-side fix, since the assertion lives in gRPC's shared
  C-core machinery and can equally be triggered by inbound RPC
  cancellations/retries. Verified this isn't a `flagd` chaos/fault-injection
  flag (only `recommendationCacheFailure` exists for this service, and it's
  unrelated) - it's a genuine, still-open upstream bug with no full fix
  available in any known grpcio version, so the workaround is applied
  symmetrically instead.
* [chart] `ad` was still crash-looping after the previous release's IPv4/IPv6
  health-check fix, but for an unrelated reason: this cluster's OBI eBPF
  DaemonSet dynamically attaches its own Java agent to every JVM in the
  `demo-pr-194` namespace, alongside the OTel Operator's own `-javaagent`
  injection this chart already configures for `ad` - two agents doing
  bytecode instrumentation concurrently at startup, under `ad`'s previous
  `300m` CPU limit, took long enough that kubelet's liveness/readiness
  probes (5s initial delay, 3 failures at 10s) killed the container before
  it ever opened a single socket. Raised `ad`'s CPU request/limit
  (`50m`/`300m` -> `200m`/`1000m`) and gave both probes more runway
  (`initialDelaySeconds` 5 -> 30, added `failureThreshold: 12`, ~150s total)
  since this chart's probe template/schema has no `startupProbe` support to
  use instead. The underlying double-instrumentation collision between the
  per-PR OBI deployment and this chart's Java auto-instrumentation
  annotation is a separate, cross-team question still worth resolving.
* [ad, email, recommendation] Fixed health-check HTTP servers binding an
  IPv4-only wildcard address (`InetSocketAddress(port)` in Java,
  `TCPServer.new(port)` in Ruby, `http.server`'s default `AF_INET` in
  Python), which kubelet's liveness/readiness probes could never reach on
  an IPv6-only pod network - the pods crash-looped forever since kubelet
  killed them for repeated `connection refused` probe failures. All three
  now explicitly bind the IPv6 wildcard (`::`), matching the dual-stack
  behavior Go's `net.Listen(":8081")` already had (e.g. `cart`, which was
  unaffected). Confirmed via a live PR preview environment where these
  three `0.11.26` pods were the only ones crash-looping.
* [telemetry-schema] Removed the orphaned `demo.shipping.items_count`
  attribute from `telemetry-schema/attributes/shipping.yaml`: it was never
  referenced by any `telemetry-schema/services/*.yaml` entry and no service
  emits it, so the generated telemetry-docs site was documenting an
  attribute that doesn't exist in practice.
* [chart] Enable `telemetry-docs` by default: it was disabled with a comment
  claiming no versioned image had ever been published for it, but the
  `component-build-images.yml` matrix has built and pushed
  `<version>-telemetry-docs` alongside every other component since the
  service was added, and `ghcr.io/ryanfaircloth/demo:0.11.26-telemetry-docs`
  is confirmed present in the registry. Added the same `imageOverride` pin
  every other component gets, plus liveness/readiness probes (it was
  disabled during the earlier chart-wide probe rollout, so it never
  received them).
* [chart] `0.11.25` never actually got published: the release commit
  only touched `.env`/`Chart.yaml`/`values.yaml`, and `component-release.yml`
  only triggered on `src/**` changes, so the workflow never ran and no
  `0.11.25-*` image exists for any component in GHCR - yet `values.yaml` now
  pins every component to that non-existent tag. Fixed the trigger to also
  fire on `.env`/`Chart.yaml`/`values.yaml` changes. Separately, `cart` has
  been failing to build since `0.11.23`: `Program.cs` calls
  `builder.WebHost.ConfigureKestrel(...)` but only imported
  `Microsoft.AspNetCore.Server.Kestrel.Core` (for `HttpProtocols`), not
  `Microsoft.AspNetCore.Hosting` where the `ConfigureKestrel` extension
  method itself lives, so the image silently never got pushed for
  `0.11.23`/`0.11.24` either. Added the missing `using`. Skipping straight to
  `0.11.26` rather than retroactively publishing `0.11.25`.
* [chart] Sync every per-component `image.tag` pin in `values.yaml`
  (`ad`, `cart`, `checkout`, `currency`, `email`, `fraud-detection`,
  `frontend`, `image-provider`, `load-generator`, `payment`,
  `product-catalog`, `quote`, `recommendation`, `shipping`, `flagd-ui`,
  `kafka`, `accounting`) from stale `0.11.17-*` tags up to `0.11.25-*`.
  The release process bumps `Chart.yaml`'s `version`/`appVersion` and
  `.env`'s `IMAGE_VERSION` on every release (see the `0.11.21` "sync
  appVersion and IMAGE_VERSION" fix), but never bumped these hardcoded
  per-component tags, so every chart release since `0.11.18` kept
  deploying `0.11.17` images regardless of the chart version - masking
  every fix landed since, including the `0.11.23` health-check fixes.
  Confirmed via a live PR preview environment still pulling
  `0.11.17-{ad,cart,checkout,currency,email,shipping}` and
  crash-looping on the exact liveness-probe failures `0.11.23` fixed.
* [recommendation] Fix the `psutil`/`mallinfo` auto-instrumentation failure
  noted below as a known gap: switched the Dockerfile from
  `python:3.14.6-alpine` to `python:3.14.6-slim-bookworm` (glibc), matching
  every other Python service in the repo, and dropped the now-unneeded
  `gcc`/`g++`/`linux-headers` build deps since manylinux wheels cover glibc.
  The operator's injected auto-instrumentation bundle ships glibc-compiled
  psutil, so its `SystemMetricsInstrumentor` now loads instead of silently
  failing to relocate `mallinfo`. Verified: builds and starts cleanly on the
  new base image.
* [flagd-ui] Add `GET /healthz` (plain 200, liveness) and `GET /readyz`
  (readiness) routes. `/readyz` checks that the `Storage` GenServer -
  which loads the shared flag config file on init and backs every route in
  the app - is alive and responding, rather than a fake always-200. Wired
  both into the chart's `livenessProbe`/`readinessProbe` for the flagd-ui
  sidecar. Also fixed `flagd-ui`'s missing `imageOverride` in `values.yaml`:
  it had no `imageOverride` at all, so it silently deployed the untouched
  upstream `ghcr.io/open-telemetry/demo` image instead of this fork's own
  build - the same latent bug previously fixed for `ad`/`image-provider`/
  `kafka` - meaning this fix (and any other fork-specific flagd-ui change)
  would never actually have been deployed via this chart otherwise.
* [chart] Remove the stale `product-reviews` and `llm` component blocks from
  `values.yaml`. Both services were removed upstream (open-telemetry/opentelemetry-demo#3587,
  #3599) with no source left in this repo to build or override, so these
  blocks had silently fallen back to pulling upstream `ghcr.io/open-telemetry/demo`
  images that likely no longer exist either - a previous release already
  noted this fact without acting on it. Also drops `frontend`'s dead
  `PRODUCT_REVIEWS_ADDR` env var (unreferenced in `frontend`'s own source)
  and regenerates the example `rendered/component.yaml` fixtures under
  `examples/*` to match.
* [chart] Wire `livenessProbe`/`readinessProbe` (httpGet) for `flagd` against
  its built-in management port (8014, default and unchanged here):
  `/healthz` for liveness (200 as soon as the process is up) and `/readyz`
  for readiness (412 until every configured sync provider - here, the
  file-backed one - completes its first successful sync, then 200
  thereafter). See <https://flagd.dev/reference/monitoring/#definition-of-readiness>.
* [chart] Wire `livenessProbe`/`readinessProbe` (httpGet) for `image-provider`
  against its existing `/status` Nginx `stub_status` endpoint. This component
  is a pure static-file server with no app logic beyond Nginx itself, so
  Nginx responding at all is the complete health signal available - no code
  change needed.
* [email, frontend, quote] Add a health endpoint to the three remaining HTTP
  services that had no health route at all, and wire `livenessProbe`/
  `readinessProbe` (httpGet) against it in the chart. `email` (Ruby/Sinatra)
  gets a `GET /healthz`, `quote` (PHP/Slim) gets a `GET /health`, and
  `frontend` (Next.js) gets a `GET /api/healthz` API route - all three reuse
  the service's existing app port, no new port needed.
* [ad, cart, checkout, currency, payment, product-catalog] Replace each
  service's gRPC health check with a plain HTTP one, and wire
  `livenessProbe`/`readinessProbe` (httpGet) against it in the chart. gRPC
  health checking libraries are the same class of fragile, version-sensitive
  dependency that broke `recommendation` under auto-instrumentation
  injection (0.11.14) - an HTTP probe needs no gRPC client tooling and
  avoids that risk for the *checking* path specifically, regardless of
  whether that particular language's health-check library shares the exact
  fragility. `ad` (Java, JDK `com.sun.net.httpserver.HttpServer` - also drops
  the now-unused `grpc-services` dependency), `checkout`/`product-catalog`
  (Go, stdlib `net/http`), `payment` (Node.js, stdlib `http` - also drops the
  now-unused `grpc-js-health-check` dependency), and `currency` (C++, a
  minimal raw-socket responder - the language has no stdlib HTTP server)
  each now listen on a new `<SERVICE>_HEALTH_PORT` (default 8081) for
  `GET /healthz`, and no longer register a gRPC health service at all.
  `cart` (.NET) instead muxes a `GET /healthz` onto its existing port by
  switching Kestrel to `Http1AndHttp2`, reusing the `readinessCheck`
  registration that used to back its gRPC health service via a plain
  `AddHealthChecks()` + `app.MapHealthChecks()` instead - the hand-rolled
  gRPC `HealthServiceImpl` and the `Grpc.AspNetCore.HealthChecks` package
  reference are both gone.
* [chart] Wire `livenessProbe`/`readinessProbe` (httpGet) for `recommendation`
  (`GET /healthz` on the newly-exposed `RECOMMENDATION_HEALTH_PORT`, 8081)
  and `shipping` (`GET /health` on its existing app port, 8080).
* [recommendation] Replace the gRPC health check (`grpc_health.v1`,
  registered via `add_HealthServicer_to_server`) with a plain stdlib HTTP
  endpoint (`GET /healthz` on `RECOMMENDATION_HEALTH_PORT`, default
  `8081`). The gRPC health check pulled in `grpcio-health-checking`'s
  bundled, pre-compiled protobuf gencode - exactly the kind of
  version-sensitive compiled dependency that broke this service under
  real auto-instrumentation injection two releases ago (0.11.14). An
  HTTP health check needs no protobuf at all, eliminating that entire
  class of fragility for the health endpoint specifically. Confirmed
  nothing in this repo's compose/chart config currently probes the gRPC
  health check (no `livenessProbe`/`readinessProbe` configured for
  recommendation), so this is a zero-impact swap; wiring an actual
  `readinessProbe` against the new HTTP endpoint is a natural follow-up,
  not done here. Verified: builds, and the new endpoint returns 200 for
  `/healthz` and 404 otherwise.
* [recommendation] Revert the grpcio/protobuf/opentelemetry version bump
  from the previous release (grpcio-health-checking 1.82.1,
  openfeature-provider-flagd 0.5.1, opentelemetry-api/sdk/
  exporter-otlp-proto-grpc 1.44.0) - confirmed via a real deployment
  under the operator's injected Python auto-instrumentation to cause a
  fatal, deterministic crash on startup. That bump required
  protobuf>=7.35.1, but the operator's injected bundle ships an older
  protobuf 6.33.6 ahead of the app's own venv on PYTHONPATH; protobuf's
  own runtime-version guard then raises `VersionError` on the very first
  import in recommendation_server.py, killing the process every time. It
  was a speculative "free to take, not confirmed to help" change to
  begin with; the previous release's grpc.enable_retries=0 channel
  option is the actually-targeted fix for the original crash and doesn't
  require any version change, so it stays. Verified: builds, resolves to
  protobuf==6.33.6 (matching what the operator's bundle ships), and
  starts cleanly.

  Also noted, not fixed at the time: the same deployment log showed
  `psutil`/`_psutil_linux.abi3.so: mallinfo: symbol not found` - the same
  musl-vs-glibc class of issue fixed for cart in 0.11.6. recommendation
  still ran on Alpine (musl); the operator's injected auto-instrumentation
  bundle ships glibc-compiled psutil, so its SystemMetricsInstrumentor
  silently failed to load (non-fatal, gracefully skipped, not a crash).
  Left as a known gap rather than switching base images in that release -
  see the newer entry above, which fixes this by moving recommendation off
  Alpine.
* [currency] Update RPC span attributes to current OpenTelemetry semantic
  conventions, found while auditing the fleet against real-world
  telemetry best practices: `rpc.system` -> `rpc.system.name`,
  `rpc.grpc.status_code` -> `rpc.response.status_code` (now a string
  value), and folded the deprecated `rpc.service` attribute into a
  fully-qualified `rpc.method` (e.g. `oteldemo.CurrencyService/Convert`)
  per the current spec. Eliminates the `OPENTELEMETRY_DEPRECATED`
  compiler warnings this produced on every build. No behavior change;
  verified the service still builds and starts cleanly.
* [recommendation] Fix a live crash loop seen in a dev cluster:
  `recommendation`'s gRPC client channel to `product-catalog` was hitting
  a longstanding native `grpcio` C-core bug - a call combiner ref-count
  assertion (`Check failed: prev_size >= 1u` in `call_combiner.cc`,
  `CallCombiner::Stop()` called more times than `Start()`), tied to
  gRPC's internal retry/cancellation machinery (see grpc/grpc#26537,
  grpc/grpc#38251). Not caused by this session's OTel migration -
  `recommendation`'s grpc code and dependency versions hadn't changed
  across any of those releases. Added `grpc.enable_retries=0` to the
  channel options as the most commonly cited mitigation for this bug
  class, and bumped `grpcio`/`grpcio-health-checking` 1.81.1 -> 1.82.1 to
  pick up any interim fixes (not confirmed to resolve this specific
  signature, but free to take). That bump required also bumping
  `openfeature-provider-flagd` 0.5.0 -> 0.5.1 and
  `opentelemetry-api`/`opentelemetry-sdk`/`opentelemetry-exporter-otlp-proto-grpc`
  1.42.0 -> 1.44.0, since `grpcio-health-checking` 1.82.1 requires
  `protobuf>=7.35.1` while the older pins of those three packages capped
  `protobuf<7.0`.
* [load-generator] Remove the manually-built `TracerProvider`/`MeterProvider`/
  `OTLPSpanExporter`/`OTLPMetricExporter`/`BatchSpanProcessor`/
  `PeriodicExportingMetricReader` bootstrap (plus the unused `Resource`
  import); traces/metrics now depend on an externally injected Python
  auto-instrumentation agent instead of a version pinned in the app's own
  dependencies, matching the rest of the fleet. Unlike every other Python
  service, the four `.instrument()` calls (`Jinja2Instrumentor`,
  `RequestsInstrumentor`, `SystemMetricsInstrumentor`, `URLLib3Instrumentor`)
  stay explicit and in their exact current position rather than being
  handed off to auto-discovery: Locust's own CLI monkey-patches the
  stdlib via gevent before loading this file, and a zero-code or injected
  bootstrap runs at interpreter startup - before that patching - so
  instrumenting `requests`/`urllib3` there risks the same ordering
  conflict this manual approach exists to avoid. If this pod is ever
  deployed under real Python auto-instrumentation injection, the injector
  needs `OTEL_PYTHON_DISABLED_INSTRUMENTATIONS=requests,urllib3` set so it
  doesn't also try to instrument those two before gevent patches. All
  `@task` business spans (`user_index`, `browse_product`,
  `get_recommendations`, etc.) and the logs bootstrap are untouched - the
  logs bootstrap already depends on `opentelemetry-sdk`/
  `opentelemetry-exporter-otlp-proto-grpc` directly, so no dependencies
  changed, only code.
* [currency] Fix `IPV6_ENABLED` check in `RunServer`: it compared a
  `const char*` from `getenv()` against a string literal by pointer
  identity (`ipv6_enabled == "true"`) rather than comparing contents, so
  the condition was always false and the IPv6 listen-address override
  never actually activated. Now uses `strcmp` with a null check. Found
  incidentally during the OTel config consistency audit; unrelated to
  telemetry. Also hardens `RunServer`'s `VERSION` env var read, found in
  the same review: `std::string version = std::getenv("VERSION")`
  constructs a string directly from a possibly-null pointer, which throws
  `std::logic_error` if `VERSION` is ever unset. Both compose and the Helm
  chart always set it today, so this hadn't been hit, but it's a real
  crash risk for anyone running the binary directly.
* [recommendation] Remove `logger.py` (`CustomJsonFormatter`/`getJSONLogger`)
  and the `python-json-logger`/`python-dotenv` dependencies it and nothing
  else pulled in - dead code, never imported by `recommendation_server.py`,
  which uses stdlib `logging` directly. Flagged during this session's OTel
  migration audit and removed as a follow-up cleanup.
* [ci] `build-images.yml` only builds/pushes images on pushes touching
  `src/**`; a chart- or doc-only release (like the 0.11.8 shipping fix)
  triggers no build at all, so bumping every pinned `imageOverride` tag in
  that situation points the chart at images that were never published.
  Added a `force_build` `workflow_dispatch` input, threaded through to the
  existing `component-build-images.yml` `force_build` support, so a
  release that doesn't naturally touch `src/**` can still force a full
  rebuild of every component.
* [helm] `ad`, `image-provider`, and `kafka` never had an `imageOverride`,
  so the chart silently deployed the upstream `ghcr.io/open-telemetry/demo`
  image for them instead of this fork's own build - meaning fork-specific
  fixes to these three (including this session's `ad` Java-agent removal)
  had never actually been deployable via this chart. Added `imageOverride`
  for all three. `kafka`'s bundled component is force-disabled by default
  (`kafkaAccess.mode: kafkaAccess`), so this was latent until someone
  opts into `mode: legacy`; `ad` and `image-provider` are enabled by
  default, so this was live-broken for anyone deploying today.
  `telemetry-docs` has the same shape of problem but was already caught
  and disabled with an explanatory comment; `flagd-ui`/`opensearch`/
  `opamp-server`/`frontend-proxy` aren't chart components at all (no
  dedicated templates), a deliberate k8s-vs-compose architecture
  difference, not a gap. `product-reviews`/`llm` have no source in this
  repo at all, so there's nothing to build or override for them.
* [helm] Extend the `resource.opentelemetry.io/service.name` annotation
  (introduced for frontend/fraud-detection in 0.11.1) to the rest of the
  operator-injectable services migrated since: `ad`, `payment`,
  `recommendation`, `accounting`, `cart`. They'd been left on the older
  `service.namespace: otel-demo` annotation, an inconsistency caught in a
  final pass over the whole migration.
* [shipping] Point the Helm chart's `OTEL_EXPORTER_OTLP_ENDPOINT` at the
  collector's HTTP port (4318) instead of its gRPC port (4317); found
  during an audit of OTel config consistency across the services that
  can't use auto-instrumentation. shipping's Rust exporters
  (`telemetry_conf.rs`) are hardcoded to `Protocol::HttpBinary` for
  traces, metrics, and logs, and never read `OTEL_EXPORTER_OTLP_PROTOCOL`,
  so posting to the gRPC-only port produced the same class of failure
  already fixed for checkout in 0.10.7. compose.yaml already had this
  right (`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` at port 4318); only
  the chart value was wrong. Audited currency, email, and quote too, and
  all three were already consistent (currency's C++ exporters are
  genuinely gRPC-only via `OtlpGrpc*ExporterFactory`, so its 4317 chart
  value is correct as-is).
* [agent] Drop the bundled `Traceloop.init()` bootstrap and the manual
  `FastAPIInstrumentor.instrument_app()`/`HTTPXClientInstrumentor().instrument()`
  calls, and their now-unused `opentelemetry-api`/`opentelemetry-sdk`/
  `opentelemetry-semantic-conventions`/`opentelemetry-instrumentation-fastapi`/
  `-requests`/`-httpx` dependencies; tracing now depends on an externally
  injected Python auto-instrumentation agent instead of a version pinned
  in the app's own dependencies. Unlike `mcp`, `agent` genuinely calls LLMs
  through `langchain`/`langchain_openai`, and `agents.py`'s
  `@workflow(name="astronomy_shop_agent_workflow")` decorator
  (`traceloop.sdk.decorators.workflow`) is real business instrumentation -
  both it and `opentelemetry-instrumentation-langchain` (the actual LLM
  span instrumentor) stay as dependencies. Neither needs an explicit
  `.instrument()`/`.init()` call: `opentelemetry-instrumentation-*`
  packages register standard entry points that any zero-code or injected
  Python auto-instrumentation bootstrap discovers automatically from the
  app's own installed packages, the same way `opentelemetry-instrumentation-requests`
  worked for `mcp`.
* [cart] Switch from code-based OTel SDK wiring to externally injected
  .NET auto-instrumentation. Unlike the other .NET services migrated so
  far, cart never used the `OpenTelemetry.AutoInstrumentation` profiler
  package - it hand-wired `OpenTelemetry.Extensions.Hosting`/
  `OpenTelemetry.Instrumentation.*` packages directly in `Program.cs`
  (`AddAspNetCoreInstrumentation()`, `AddGrpcClientInstrumentation()`,
  `AddRedisInstrumentation()`, `AddOtlpExporter()`, etc.). This required
  two changes:
  * The base image was Alpine/musl, self-contained, single-file - the OTel
    Operator's .NET auto-instrumentation profiler is a glibc-linked native
    library and can't load there at all. Switched to a glibc runtime
    (`mcr.microsoft.com/dotnet/aspnet`) with a normal framework-dependent
    publish. This uncovered a pre-existing gap in the demo: no other
    manually-instrumented service in this repo is in a language the OTel
    Operator actually supports, so cart's musl build had never been
    exercised against real injection before.
  * Removed all `OpenTelemetry.*` packages and the `Program.cs` SDK/
    resource-detector wiring, including the `StackExchangeRedisInstrumentation`
    connection registration workaround that code-based Redis instrumentation
    required (profiler-based Redis instrumentation attaches automatically,
    no registration needed). Added `OTEL_DOTNET_AUTO_TRACES_ADDITIONAL_SOURCES`/
    `OTEL_DOTNET_AUTO_METRICS_ADDITIONAL_SOURCES` so the profiler picks up
    cart's own `ActivitySource`/`Meter` ("OpenTelemetry.Demo.Cart") and
    OpenFeature's meter, and `OTEL_METRICS_EXEMPLAR_FILTER=trace_based` to
    preserve the previous exemplar behavior. `Directory.Packages.props` no
    longer centrally pins any `OpenTelemetry.*` package version either.
  * Known fidelity gap: the removed `AddRedisInstrumentation(options =>
    options.SetVerboseDatabaseStatements = true)` call captured full Redis
    command text as a span attribute; the auto-instrumentation profiler's
    Redis integration doesn't expose an equivalent toggle, so that specific
    attribute is not expected to appear anymore.
  * `CartService.cs`'s manual `Activity.Current?.SetTag(...)` calls and
    `ValkeyCartStore.cs`'s custom `Histogram`s are untouched - both use
    .NET's built-in `System.Diagnostics` APIs directly, which work under
    any instrumentation mechanism.
  * Like the other migrated .NET/Node services, cart gets no traces under
    `docker compose up` (no bundled agent, no injection mechanism there);
    the Helm-chart/operator path is unaffected.
* [recommendation] Move from a self-baked zero-code setup (own venv,
  `opentelemetry-bootstrap -a install`, `opentelemetry-instrument` as
  entrypoint wrapper, `opentelemetry-distro`/`psutil` dependencies) to
  fully externally injected instrumentation, matching the other migrated
  services: the venv still installs `opentelemetry-api`/`opentelemetry-sdk`
  explicitly (needed directly by business code and the retained manual
  logs pipeline), but the zero-code bootstrap and its bundled-agent
  dependencies are gone. Business spans/attributes in
  `get_product_list`/`ListRecommendations`, the `demo.recommendation.requests`
  counter, and the manual `LoggerProvider`/`OTLPLogExporter` logs setup are
  all untouched - the logs bootstrap intentionally stays manual for now,
  since correlating stdout logs with trace context across every service is
  a separate, not-yet-started workstream.
* [accounting] Drop the `OpenTelemetry.AutoInstrumentation` NuGet package
  and its `instrument.sh` profiler-wrapper entrypoint; tracing now depends
  on an externally injected .NET auto-instrumentation profiler instead of
  a version pinned in the csproj. Uncovered and fixed a latent bug in the
  process: `Consumer.cs`/`Program.cs` use `Microsoft.Extensions.Hosting`'s
  `BackgroundService` but never referenced the package directly - it only
  ever compiled because `OpenTelemetry.AutoInstrumentation` pulled it in
  transitively. Added an explicit `Microsoft.Extensions.Hosting`
  `PackageReference`. `Consumer.cs`'s manual `ActivitySource("Accounting.Consumer")`
  span and the `OTEL_DOTNET_AUTO_TRACES_ADDITIONAL_SOURCES` env var that
  tells the profiler to listen to it are untouched - that config is needed
  regardless of whether the profiler is bundled or injected. Like payment,
  accounting gets no traces under `docker compose up` (no injection
  mechanism there); the Helm-chart/operator path is unaffected.
* [payment] Drop the bundled `@opentelemetry/auto-instrumentations-node`
  and its supporting `@opentelemetry/sdk-node`/exporter/resource-detector
  dependencies; tracing now depends on an externally injected Node.js
  auto-instrumentation agent instead of a version pinned in the app's own
  dependencies. `@opentelemetry/api` stays, since `charge.js`/`index.js`
  call it directly for business spans/attributes and the
  `demo.payment.transactions` counter. Also removes payment's
  `NODE_OPTIONS=--require @opentelemetry/auto-instrumentations-node/register`
  from `compose.yaml`: unlike the Helm chart (where a real operator injects
  Node.js instrumentation), docker-compose has no equivalent mechanism, so
  that env var would have crash-looped payment the moment it stopped
  bundling the package itself. `payment` gets no traces under
  `docker compose up` until compose gains an injection story of its own;
  the Helm-chart/operator path is unaffected. pino's OTel logs transport is
  unrelated to this change and stays as-is.
* [ad] Drop the bundled OTel Java agent jar and its `-javaagent`
  `JAVA_TOOL_OPTIONS` flag from the image; tracing now depends on an
  externally injected Java auto-instrumentation agent instead of a jar
  version pinned in the Dockerfile. `ad`'s business spans/metrics
  (`GlobalOpenTelemetry.getTracer`/`getMeter`, the `@WithSpan`-annotated
  `getAdsByCategory`, `demo.ad.requests` counter) are untouched, and
  `@WithSpan` continues to work as before since it's woven in by whichever
  Java agent is attached at runtime, bundled or injected.
* [frontend] Drop the bundled Node.js OTel SDK bootstrap
  (`Instrumentation.js`, `@opentelemetry/sdk-node`,
  `auto-instrumentations-node`, OTLP exporters, and resource detectors) and
  the `--require` hook that loaded it. Server-side tracing/metrics now
  depend on an externally injected Node.js auto-instrumentation agent
  instead of a version pinned in the app's own dependencies. Browser-side
  telemetry (`FrontendTracer.ts`) is unaffected.
* [fraud-detection] Drop the bundled OTel Java agent jar and its
  `-javaagent` `JAVA_TOOL_OPTIONS` flag from the image; tracing now depends
  on an externally injected Java auto-instrumentation agent instead of a
  jar version pinned in the Dockerfile.
* [helm] For `frontend` and `fraud-detection`, replace the
  `resource.opentelemetry.io/service.namespace` pod annotation with
  `resource.opentelemetry.io/service.name`, matching the pattern an
  injected auto-instrumentation agent reads for resource attributes.
  Service version is left to the standard `app.kubernetes.io/version` pod
  label instead of a duplicate annotation, and `deployment.environment.name`
  is intentionally left unset here since it's environment-specific and
  belongs in each deployer's own values override, not a chart default.
* [chatbot] Drop the bundled OTel Python bootstrap (manual `TracerProvider`,
  `OTLPSpanExporter`, `RequestsInstrumentor`, `HTTPXClientInstrumentor`) and
  the now-unused `opentelemetry-*` dependencies; tracing now depends on an
  externally injected Python auto-instrumentation agent instead of a
  version pinned in the app's own dependencies. No business spans or custom
  metrics existed in this service, so nothing else changes.
* [mcp] Drop the bundled `traceloop-sdk` bootstrap (`Traceloop.init()`,
  `HTTPXClientInstrumentor`) and its now-unused dependencies
  (`traceloop-sdk`, `opentelemetry-*`, and the `langchain`/`langgraph`
  dependency tree that existed solely to be instrumented and was never
  imported by mcp's own code); tracing now depends on an externally
  injected Python auto-instrumentation agent instead of a version pinned
  in the app's own dependencies. `opentelemetry-api` remains as a
  transitive dependency of `fastmcp` itself, unrelated to this change.
* [checkout] Point the Helm chart's `OTEL_EXPORTER_OTLP_ENDPOINT` at the
  collector's HTTP port (4318) instead of its gRPC port (4317); `checkout`'s
  Go SDK exporters speak `http/protobuf` only, and posting HTTP requests at
  the gRPC listener produced malformed-response errors on every trace,
  metric, and log export.
* [kafka] `accounting` and `fraud-detection` now read their consumer
  `group.id` from `KAFKA_CONSUMER_GROUP` instead of a hardcoded value, so
  environments sharing a cluster but subscribed to different per-env topics
  no longer collide on the same group id.
* [load-generator] Drop the unused `g++` build dependency; every package in
  `requirements.txt` installs from a prebuilt wheel, so no source
  compilation ever happens.
* [grafana] Remove the bundled Grafana service, dashboards, and datasource
  provisioning from the fork. Jaeger and OpenSearch stay, since CI's
  `test/telemetry` suite queries them directly to verify traces and logs.
* [checkout] Remove a dead gRPC client dial for `shipping` left over from its
  migration to a REST call (`quoteShipping`/`shipOrder` already POST to
  `shipping` over HTTP). The unused dial blocked startup waiting for a gRPC
  handshake that `shipping`'s actix-web REST server never speaks, crash-looping
  `checkout` with `TRANSIENT_FAILURE` even while `shipping` itself was healthy.
* [checkout] Remove the same dead gRPC client dial for `email`, which is also
  called over REST (`sendOrderConfirmation` already POSTs to `email` over
  HTTP). The unused dial blocked startup on a gRPC handshake `email`'s
  Sinatra REST server never speaks, crash-looping `checkout` with
  `TRANSIENT_FAILURE` even while `email` itself was healthy.
* [kafka] Add `KAFKA_TOPIC` environment variable to configure the Kafka topic
  name used by `checkout`, `accounting`, and `fraud-detection`, defaulting to
  `orders` to preserve existing behavior
* [compose] Run `checkout`, `product-catalog`, and `shipping` with a
  read-only root filesystem (`read_only: true` plus a `/tmp` tmpfs mount),
  for container platforms that prohibit writable root filesystems. Limited
  to these three services for now since they're the ones verified to have
  no runtime file writes of their own; other services write files at
  startup and need dedicated tmpfs mounts before they can be switched over
  ([#1731](https://github.com/open-telemetry/opentelemetry-demo/issues/1731))
* [llm] Increase `llm` service memory limit from 50M to 100M to prevent a
  startup restart loop caused by the container exceeding its memory limit
  ([#2944](https://github.com/open-telemetry/opentelemetry-demo/issues/2944))
* [telemetry-docs] Add a new service to provide telemetry documentation based
  on Weaver
  ([#2794](https://github.com/open-telemetry/opentelemetry-demo/pull/2794))
* [accounting] fix memory leak with dbcontext
  ([#2876](https://github.com/open-telemetry/opentelemetry-demo/pull/2876))
* [chore] Upgrade OTel Collector to v0.145.0 with :warning: breaking change:
  OTLP exporters renamed from `otlp` to `otlp_grpc/jaeger` and from
  `otlphttp/prometheus` to `otlp_http/prometheus`
  [#2942](https://github.com/open-telemetry/opentelemetry-demo/pull/2942)
* [collector] Use the
  [`set_semconv_span_name()`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/transformprocessor#set_semconv_span_name)
  function to better handle the next.js issue
  [High-cardinality HTTP span names #54694](https://github.com/vercel/next.js/issues/54694)
  [#2942](https://github.com/open-telemetry/opentelemetry-demo/pull/2942)
* add `main` tagged images, drop date suffix for `nightly`
  ([#2994](https://github.com/open-telemetry/opentelemetry-demo/pull/2994))
* [docker] fix `docker-compose.minimal.yml` to be able to run by adding missing
  postgresql service, environment variables, and dependencies
  ([#3004](https://github.com/open-telemetry/opentelemetry-demo/pull/3004))
* [chore] Bump dependent image versions to latest releases
  ([#3005](https://github.com/open-telemetry/opentelemetry-demo/pull/3005))
* [flagd-ui] fix memory issue with BEAM-VM, this reduces flagd-ui memory
  usage from 2.3GB to 228Mi
  [#3022](https://github.com/open-telemetry/opentelemetry-demo/pull/3022)
* [ad] and [fraud-detection] Service JVM heap set to 200m for ad service and
  180m for fraud-detection to prevent large heap size that causes
  OOMKills with k8s.
  ([#3105](https://github.com/open-telemetry/opentelemetry-demo/pull/3105))
* [postgresql] More realistic PostgreSQL setup: replace generic `root`/`otelu` users
  and `otel` database with dedicated `astronomy_db` owned by `astronomy_user`;
  add `monitoring_user` with `pg_monitor` role for the OTel Collector receiver;
  enable `pg_stat_statements` on all databases; rename Compose service and
  container to `astronomy-db`
  ([#3153](https://github.com/open-telemetry/opentelemetry-demo/pull/3153))
* [product-catalog] Enrich DB spans and metrics with `server.address` and `server.port`
  attributes extracted from the DSN via `otelsql.AttributesFromDSN`
  ([#3154](https://github.com/open-telemetry/opentelemetry-demo/pull/3154))
* [otelcollector] add kafkametricsreceiver
  ([#3158](https://github.com/open-telemetry/opentelemetry-demo/pull/3158))
* [load-generator] Wait for Roof Binoculars image to load in web tasks, and fix
  task failures due to missing `tracer` attribute
  ([#3171](https://github.com/open-telemetry/opentelemetry-demo/pull/3171))
* [docker] Refactor Docker Compose to use layered `-f` files with `start`,
  `start-minimal`, `start-no-o11y`, and `start-minimal-no-o11y` make targets
  ([#3229](https://github.com/open-telemetry/opentelemetry-demo/pull/3229))
* [kubernetes] Removed generated Kubernetes manifests in favor of docs
  ([#3236](https://github.com/open-telemetry/opentelemetry-demo/pull/3236))
* [cart] Swap the deprecated `OpenFeature.Contrib.Providers.Flagd` package
  provider with the new `OpenFeature.Providers.Flagd` package.
  ([#3247](https://github.com/open-telemetry/opentelemetry-demo/pull/3247))
* [recommendation] Fix `recommendationCacheFailure` feature flag by
  using `ListProducts` instead of `GetProduct`
  ([#3260](https://github.com/open-telemetry/opentelemetry-demo/pull/3260))
* [payment] Fix `charge` span lifecycle and exception attribution: wrap charge
  logic in `try/catch/finally` to ensure the span is always ended, record
  exceptions on the `charge` span where they originate, and remove duplicate
  `recordException` from the gRPC handler
  ([#3276](https://github.com/open-telemetry/opentelemetry-demo/pull/3276))
* [frontend] fix: handle undefined product images across multiple components
  ([#3291](https://github.com/open-telemetry/opentelemetry-demo/pull/3291))
* [grafana] Bump Grafana image to 13.0.1 and provision the
  `grafana-default-email` contact point explicitly, since Grafana no longer
  auto-seeds it (removed in 12.4+)
* [react-native-app] Update to Expo 55.0.16
  ([#3296](https://github.com/open-telemetry/opentelemetry-demo/pull/3296))
* [docker] Podman doesn't support the tag feature of docker logs,
  for the otel-demo to support podman we need to remove the tag from docker logs.
  [#3304](https://github.com/open-telemetry/opentelemetry-demo/pull/3304)
* [podman] Add podman support to run the demo. The makefile has been updated
  to detect what container runtime is installed.
  [#3307](https://github.com/open-telemetry/opentelemetry-demo/pull/3307)
* [frontend] fix: handle corrupted session data in localStorage
  ([#3313](https://github.com/open-telemetry/opentelemetry-demo/pull/3313))
* [collector] Add `transform/sanitize_logs` processor to work around
  `otelcol.signal` scope attribute conflict with `otelcol.signal.output`
  that causes OpenSearch/Elasticsearch mapping failures
  ([#3321](https://github.com/open-telemetry/opentelemetry-demo/pull/3321))
* [profiling] Add profiling and use firepit as the backend to ingest profiles.
  This allows us to view profiles in the firepit webui.
  [#3333](https://github.com/open-telemetry/opentelemetry-demo/pull/3333)
* [shipping] Add `intlShippingSlowdown` integer feature flag to delay
  international (non-US) shipments by N seconds via flagd with OpenTelemetry tracing
  ([#3354](https://github.com/open-telemetry/opentelemetry-demo/issues/3354))
* [telemetry] Rename the product identifier telemetry attribute from
  `app.product.id` to `demo.product.id` across cart, product-catalog,
  product-reviews, telemetry schema, and trace tests.
  ([#3355](https://github.com/open-telemetry/opentelemetry-demo/pull/3355))
* [testing] Add telemetry sanity tests to validate end-to-end observability
  pipeline, including service-to-service edge verification via Jaeger trace walks
  ([#3356](https://github.com/open-telemetry/opentelemetry-demo/pull/3356))
* [frontend,ad,payment] Propagate `enduser.id` as a span attribute on all
  browser spans via `SessionIdProcessor`, forward it via W3C Baggage on
  outgoing API requests through the ApiGateway proxy, and extract it in the
  ad and payment backend services to stamp their own spans
  ([#3366](https://github.com/open-telemetry/opentelemetry-demo/pull/3366))
* [telemetry] Rename the product name telemetry attribute from
  `app.product.name` to `demo.product.name` across product-catalog and
  telemetry schema.
  ([#3370](https://github.com/open-telemetry/opentelemetry-demo/pull/3370))
* [telemetry] Rename the product quantity telemetry attribute from
  `app.product.quantity` to `demo.product.quantity` across cart and
  telemetry schema.
  ([#3371](https://github.com/open-telemetry/opentelemetry-demo/pull/3371))
* [telemetry] Rename the product review question telemetry attribute from
  `app.product.question` to `demo.product.review.question` across
  product-reviews and telemetry schema.
  ([#3372](https://github.com/open-telemetry/opentelemetry-demo/pull/3372))
* [telemetry] Rename product count telemetry attributes from
  `app.products.count` to `demo.product.count` and from
  `app.products_recommended.count` to `demo.product.recommended.count` across
  recommendation, telemetry schema, and trace tests.
  ([#3374](https://github.com/open-telemetry/opentelemetry-demo/pull/3374))
* [telemetry] Rename product filtering, search, and review telemetry attributes:
  `app.filtered_products.count` to `demo.product.filtered.count`,
  `app.filtered_products.list` to `demo.product.filtered.list`,
  `app.products_search.count` to `demo.product.search.count`,
  `app.product_reviews.count` to `demo.product.review.count`, and
  `app.product_reviews.average_score` to `demo.product.review.average_score`
  across product-catalog, product-reviews, recommendation, telemetry schema,
  and trace tests.
  ([#3376](https://github.com/open-telemetry/opentelemetry-demo/pull/3376))
* [telemetry] Rename advertising telemetry attributes:
  `app.ads.category` to `demo.ad.category`,
  `app.ads.count` to `demo.ad.count`,
  `app.ads.contextKeys` to `demo.ad.context_keys`,
  `app.ads.contextKeys.count` to `demo.ad.context_keys.count`,
  `app.ads.ad_request_type` to `demo.ad.request_type`, and
  `app.ads.ad_response_type` to `demo.ad.response_type` across ad,
  product-catalog, product-reviews, recommendation, telemetry schema, and trace
  tests.
  ([#3387](https://github.com/open-telemetry/opentelemetry-demo/pull/3387))
* [ad] Expose a Prometheus `/metrics` endpoint (default port `9465`) using the
  Prometheus Java client library, with a custom counter
  `demo_ad_served_total{category}`, and scrape it from the OTel Collector via a
  new `prometheus/ad` receiver to demo bridging non-OTel custom metrics into an
  OpenTelemetry pipeline.
  ([#3388](https://github.com/open-telemetry/opentelemetry-demo/pull/3388))
* [telemetry] Rename order and cart telemetry attributes:
  `app.order.id` to `demo.order.id`,
  `app.order.amount` to `demo.order.amount`,
  `app.order.items.count` to `demo.order.items.count`, and
  `app.cart.items.count` to `demo.cart.items.count` across cart, checkout,
  email, telemetry schema, and trace tests.
  ([#3389](https://github.com/open-telemetry/opentelemetry-demo/pull/3389))
* [telemetry] Rename payment telemetry attributes:
  `app.payment.amount` to `demo.payment.amount`,
  `app.payment.card_type` to `demo.payment.card_type`,
  `app.payment.card_valid` to `demo.payment.card_valid`,
  `app.payment.charged` to `demo.payment.charged`,
  `app.payment.transaction.id` to `demo.payment.transaction.id`, and
  `app.payment.currency` to `demo.payment.currency` across checkout, payment,
  and telemetry schema.
  ([#3390](https://github.com/open-telemetry/opentelemetry-demo/pull/3390))
* [telemetry] Rename shipping and quote telemetry attributes:
  `app.shipping.amount` to `demo.shipping.amount`,
  `app.shipping.cost.total` to `demo.shipping.cost.total`,
  `app.shipping.items_count` to `demo.shipping.items_count`,
  `app.shipping.tracking.id` to `demo.shipping.tracking.id`,
  `app.quote.items.count` to `demo.shipping.quote.items_count`, and
  `app.quote.cost.total` to `demo.shipping.quote.cost.total` across checkout,
  quote, shipping, and telemetry schema.
  ([#3391](https://github.com/open-telemetry/opentelemetry-demo/pull/3391))
* [telemetry] Rename exchange, user context, recommendation, notification, and
  synthetic-request telemetry attributes and metrics:
  `app.currency.conversion.from` to `demo.exchange.from`,
  `app.currency.conversion.to` to `demo.exchange.to`,
  `app.user.id` to the semconv `user.id` (removed from the demo schema),
  `app.user.currency` to `demo.user_context.selected_currency`,
  `app.loyalty.level` to `demo.user_context.loyalty_level`,
  `app.recommendation.cache_enabled` to `demo.feature_flag.recommendation_cache`,
  `app.cache_hit` to `demo.recommendation.cache_hit`,
  `app_recommendations_counter` to `demo.recommendation.requests`,
  `app.email.recipient` to `demo.notification.recipient`,
  `app.confirmation.counter` to `demo.notification.confirmations`, and
  `app.synthetic_request` to `demo.synthetic_request` across currency,
  checkout, recommendation, email, load-generator, frontend, and telemetry
  schema.
  ([#3393](https://github.com/open-telemetry/opentelemetry-demo/pull/3393))
* [AI Agents] Added guidance for AI Agents
  ([#3404](https://github.com/open-telemetry/opentelemetry-demo/pull/3404))
* [collector] Add resource attributes to `resourcedetection/processor` and
  updated Grafana dashboards.
  ([#3417](https://github.com/open-telemetry/opentelemetry-demo/pull/3417))
* [make] Fix `SERVICE=` alias for `build`, `restart`, and `redeploy` targets
  so the documented uppercase form actually dispatches to a single service,
  and clean up the no-arg error message that was being mangled by backticks.
  ([#3422](https://github.com/open-telemetry/opentelemetry-demo/pull/3422))
* [telemetry] Clean up remaining demo telemetry naming leftovers from
  [#3267](https://github.com/open-telemetry/opentelemetry-demo/issues/3267):
  document Ad service metric `demo.ad.requests`, rename the shipping items
  metric to `demo.shipping.items_shipped`, and replace the `currency_code`
  attribute on `demo.exchange.conversions` with `demo.exchange.to`.
  ([#3435](https://github.com/open-telemetry/opentelemetry-demo/pull/3435))
* [grafana] Update the cart exemplars dashboard to use the renamed
  `demo_cart_*` Prometheus metrics.
  ([#3436](https://github.com/open-telemetry/opentelemetry-demo/pull/3436))
* [telemetry-docs] Fix the `attr_category` macro in the service template
  (`service.md.j2`) to bucket attributes by their current `demo.*` prefixes
  instead of the old `app.*` ones.
  ([#3440](https://github.com/open-telemetry/opentelemetry-demo/pull/3440))
* [telemetry-schema] Move ad attributes into a dedicated schema domain.
  ([#3454](https://github.com/open-telemetry/opentelemetry-demo/pull/3454))
* [agent] Add Agent, MCP and ChatBot to Otel Demo application.
  Agent - Langgraph ReAct agent which can accept user requests then with the
  help of LLM call, it identifies the right set of tools. Agent also has an
  LLM response caching feature.
  MCP - Agent can be configured to use MCP tools or native langgraph tools to
  interact with demo application.
  Chatbot - facilitates an interactive UI for users to send requests to agent.
  ([#3455](https://github.com/open-telemetry/opentelemetry-demo/pull/3455))
* [telemetry-schema] Split exchange, feature flag, recommendation, and request
  attributes into dedicated schema domains.
  ([#3482](https://github.com/open-telemetry/opentelemetry-demo/pull/3482))
* [telemetry] Split cart and payment attributes out of the order telemetry
  schema into their own domain files.
  ([#3484](https://github.com/open-telemetry/opentelemetry-demo/pull/3484))
* [payment] Replace manual SDK initialization with zero-code instrumentation
  via `NODE_OPTIONS=--require @opentelemetry/auto-instrumentations-node/register`
  ([#3486](https://github.com/open-telemetry/opentelemetry-demo/pull/3486))
* [chore] Add health check to services
  ([#3487](https://github.com/open-telemetry/opentelemetry-demo/pull/3487))
* [testing] Telemetry tests now build the PR's images and share them across the
  full/minimal jobs via an artifact (instead of pulling released images), wait
  for the traces, metrics, and logs backends during warmup to avoid per-test
  timeouts, and cap the test step at 15 minutes
  ([#3498](https://github.com/open-telemetry/opentelemetry-demo/pull/3498))
* [fraud-detection] fix gRPC service files dropped from the shadow jar by
  setting `duplicatesStrategy` to `INCLUDE`, restoring the DNS name resolver
  registration needed to connect to flagd
  ([#3501](https://github.com/open-telemetry/opentelemetry-demo/pull/3501))
* [testing] Telemetry test warmup now drives a few real checkouts through the
  frontend so low-frequency services (`email`, `quote`) that only emit on the
  checkout path produce telemetry deterministically, instead of depending on the
  load generator's ~6% checkout task weight landing inside the test window
  ([#3505](https://github.com/open-telemetry/opentelemetry-demo/pull/3505))
* [fraud-detection] Set the Kafka consumer `auto.offset.reset` to `earliest` so
  it processes `orders` produced before its consumer group finished joining,
  matching the accounting consumer. Previously the Kafka default of `latest`
  silently dropped those orders, so fraud-detection could emit no telemetry on a
  quiet or cold start
  ([#3505](https://github.com/open-telemetry/opentelemetry-demo/pull/3505))
* [frontend] Fix cart page showing unit price instead of line total; add
  per-item quantity selector so users can change quantities directly in the
  cart, with each row now displaying both the unit price and the line total
  (unit price * quantity)
  ([#3521](https://github.com/open-telemetry/opentelemetry-demo/pull/3521))
* [cart,accounting] Use source-generated logging with EventName
  ([#3559](https://github.com/open-telemetry/opentelemetry-demo/pull/3559))
* [opamp] Add an OpAMP server and configure the Collector to report status,
  version, attributes, and effective configuration through the OpAMP extension
  ([#3566](https://github.com/open-telemetry/opentelemetry-demo/pull/3566))
* [cleanup] Remove loadgen traffic to product reviews
  ([#3567](https://github.com/open-telemetry/opentelemetry-demo/pull/3567))
* [cleanup] Remove product reviews from frontend
  ([#3568](https://github.com/open-telemetry/opentelemetry-demo/pull/3568))
* [frontend-proxy] Pass `CHATBOT_HOST`/`CHATBOT_PORT` to the frontend-proxy in
  the base compose file. The chatbot upstream cluster lives in the base
  `envoy.tmpl.yaml` and `envsubst` runs in-container, so without these vars the
  proxy rendered an empty chatbot address and failed Envoy bootstrap validation
  whenever the agent stack was not layered on
  ([#3570](https://github.com/open-telemetry/opentelemetry-demo/pull/3570))
* fix(frontend-proxy): remove deprecated Envoy options and restore
  service.namespace resource attribute
  ([#3573](https://github.com/open-telemetry/opentelemetry-demo/pull/3573))
* [frontend-proxy] Use the asynchronous c-ares DNS resolver for Envoy upstream
  clusters instead of the blocking `getaddrinfo` resolver. With `getaddrinfo`, a
  slow or unanswered DNS lookup for an upstream that is not running (e.g. the
  `chatbot` or `profiles`/firepit clusters when the agent and profiling stacks
  are not layered on) blocked cluster warming, so Envoy never finished
  initializing its listener and the proxy never became healthy. c-ares resolves
  off the main thread, so the listener binds immediately regardless of upstream
  DNS state
  ([#3573](https://github.com/open-telemetry/opentelemetry-demo/pull/3573))
* [shipping] Add host resource detection to enrich SDK resource metadata
  ([#3581](https://github.com/open-telemetry/opentelemetry-demo/pull/3581))
* [frontend] Avoid hardcoded `localhost:8080` image URLs during SSR and
  normalize leading slashes in the custom image loader
  ([#3582](https://github.com/open-telemetry/opentelemetry-demo/pull/3582))
* [cleanup] Remove product-reviews service
  ([#3587](https://github.com/open-telemetry/opentelemetry-demo/pull/3587))
* [cleanup] Remove LLM service
  ([#3599](https://github.com/open-telemetry/opentelemetry-demo/pull/3599))
* [cleanup] Remove tracetest
([#3602](https://github.com/open-telemetry/opentelemetry-demo/pull/3602),
  [#3603](https://github.com/open-telemetry/opentelemetry-demo/pull/3603))
* [accounting] Run the Kafka consumer as a hosted background service so process
  shutdown can stop the consumer cleanly
  ([#3608](https://github.com/open-telemetry/opentelemetry-demo/pull/3608))
* [chore] Add tests to agentic services
  ([#3611](https://github.com/open-telemetry/opentelemetry-demo/pull/3611))
* [payment] Annotate synthetic load-generator payment requests with the
  `user_agent.synthetic.type` semantic convention attribute.
  ([#3613](https://github.com/open-telemetry/opentelemetry-demo/pull/3613))
* [grafana] Add exemplar-to-logs navigation: metric exemplars now link to the
  Demo Dashboard's Log Records panel, filtered to the exemplar's trace ID. The
  `Service` filter is now multi-select with an "All" option so the trace's
  logs across every service involved are shown by default, with the option to
  narrow back down to a single service.
  ([#3617](https://github.com/open-telemetry/opentelemetry-demo/pull/3617))
* [checkout] Migrate OTLP exporters (traces, metrics, logs) from gRPC to
  http/protobuf
  ([#3618](https://github.com/open-telemetry/opentelemetry-demo/pull/3618))
* [shipping] Migrate OTLP exporters (traces, metrics, logs) from gRPC to
  http/protobuf
  ([#3619](https://github.com/open-telemetry/opentelemetry-demo/pull/3619))
* [grafana] Add a "Self-Observability" dashboard that visualizes the internal
  metrics emitted by the OpenTelemetry SDKs themselves (experimental
  `otel.sdk.*` semantic conventions), and opt the `ad`, `fraud-detection` and
  `kafka` (Java) services in to SDK self-monitoring via
  `OTEL_EXPERIMENTAL_SDK_TELEMETRY_VERSION=latest`.
  The dashboard is driven by a `Service` template variable, so any additional
  service that opts in appears automatically.
  ([#3620](https://github.com/open-telemetry/opentelemetry-demo/pull/3620),
  [#3653](https://github.com/open-telemetry/opentelemetry-demo/pull/3653))
* [cart] Make the `cartFailure` feature flag rate configurable as percentage
  (off - no failures, 10%, 25%, 50%, 75%, 90%, 100% - always fail) instead of
  a fixed all-or-nothing toggle, matching the `paymentFailure` pattern
  ([#3625](https://github.com/open-telemetry/opentelemetry-demo/pull/3625))
* [collector] Add `gen_ai_normalizer` processor to the traces pipeline to
  convert OpenLLMetry/Traceloop instrumentation telemetry (from the agent
  service) into official GenAI semantic conventions (`gen_ai.*` attributes).
  Bump collector-contrib to v0.155.0 which includes the processor
  ([#3526](https://github.com/open-telemetry/opentelemetry-demo/issues/3526))
* [load-generator] Fix `synthetic_request` (and `session.id`) baggage being
  discarded before reaching any backend service, due to the baggage-bearing
  context being attached inside a span's `with` block so that the span's exit
  detached past it. Regression introduced in #2265
  ([#3627](https://github.com/open-telemetry/opentelemetry-demo/pull/3627))
* [checkout] Annotate synthetic load-generator orders with the
  `user_agent.synthetic.type` semantic convention attribute on the `PlaceOrder`
  span.
  ([#3628](https://github.com/open-telemetry/opentelemetry-demo/pull/3628))
* [load-generator] Lower the `WebsiteBrowserUser` weight so only a small
  proportion of virtual users spawn a persistent headless Chromium process
  when browser traffic is enabled, instead of scaling 1:1 with `LOCUST_USERS`
  and exhausting the container's memory limit. Ratio is configurable via the
  new `LOCUST_HTTP_USER_WEIGHT` and `LOCUST_BROWSER_USER_WEIGHT` variables,
  documented in the load generator's README
  ([#3632](https://github.com/open-telemetry/opentelemetry-demo/pull/3632))
* [email] Set `event_name` on the order-confirmation log record
  (`email.confirmation_sent`), using the OTel Ruby Logs API's `event_name`
  parameter directly.
  ([#3633](https://github.com/open-telemetry/opentelemetry-demo/pull/3633))
* [profiles] Add resource attributes to profiles
  ([#3659](https://github.com/open-telemetry/opentelemetry-demo/pull/3659))

## 2.2.0

* [feat] add ipv6 support
  ([#2594](https://github.com/open-telemetry/opentelemetry-demo/pull/2594))
* [chore] Use pre-built nginx otel image
  ([#2614](https://github.com/open-telemetry/opentelemetry-demo/pull/2614))
* [grafana] Update grafana version to 12.2.0
  ([#2615](https://github.com/open-telemetry/opentelemetry-demo/pull/2615))
* [frontend] Fix navigation and cart math
  ([#2660](https://github.com/open-telemetry/opentelemetry-demo/pull/2660))
* [feat] Add Product Review service with AI-generated summaries
  ([#2663](https://github.com/open-telemetry/opentelemetry-demo/pull/2663))
* [chore] Upgrade OpenFeature and add fix deprecation warnings for dependency
  injection
  ([#2644](https://github.com/open-telemetry/opentelemetry-demo/pull/2644))
* [frontend] fix item calculation and shipping
  ([#2684](https://github.com/open-telemetry/opentelemetry-demo/pull/2684))
* [flagd-ui] add back legacy REST APIs to empower programmatic usage
  ([#2720](https://github.com/open-telemetry/opentelemetry-demo/pull/2720))
* [collector] Remove batch processor
  ([#2734](https://github.com/open-telemetry/opentelemetry-demo/pull/2734))
* [email] Add OTLP metrics and logs
  ([#2737](https://github.com/open-telemetry/opentelemetry-demo/pull/2737))
* [collector] [dockerstats/receiver] Set API version to 1.44
  ([#2767](https://github.com/open-telemetry/opentelemetry-demo/pull/2767))
* [cart] Add health check endpoint
  ([#2830](https://github.com/open-telemetry/opentelemetry-demo/pull/2830))
* [product-catalog] Use Postgres database for products
  ([#2859](https://github.com/open-telemetry/opentelemetry-demo/pull/2859))

## 2.1.3

* [chore] Fix postgresql container
  ([#2597](https://github.com/open-telemetry/opentelemetry-demo/pull/2597))

## 2.1.2

* [chore] add postgresql and opensearch containers to build workflow
  ([#2595](https://github.com/open-telemetry/opentelemetry-demo/pull/2595))

## 2.1.1

* Align env vars
  ([#2582](https://github.com/open-telemetry/opentelemetry-demo/pull/2582))
* [opensearch] Reduce OpenSearch container memory footprint
  ([#2587](https://github.com/open-telemetry/opentelemetry-demo/pull/2587))

## 2.1.0

* [chore] add GOMEMLIMIT to all Go services
  ([#2148](https://github.com/open-telemetry/opentelemetry-demo/pull/2148))
* [product-catalog] Simplify span event name
  ([#2150](https://github.com/open-telemetry/opentelemetry-demo/pull/2150))
* [cart] Refactor OpenFeature integration and add Dependency Injection support
  ([#2160](https://github.com/open-telemetry/opentelemetry-demo/pull/2160))
* [checkout]: change image from alpine to distroless to reduce size
  ([#2161](https://github.com/open-telemetry/opentelemetry-demo/pull/2161))
* [product-catalog]: change image from alpine to distroless to reduce size
  ([#2161](https://github.com/open-telemetry/opentelemetry-demo/pull/2161))
* [grafana] configure `traceToLogs` integration
  ([#2162](https://github.com/open-telemetry/opentelemetry-demo/pull/2162))
* [recommendation] change image from bookworm to alpine to reduce size
  ([#2164](https://github.com/open-telemetry/opentelemetry-demo/pull/2164))
* [fraud-detection] update distroless to debian12
  ([#2170](https://github.com/open-telemetry/opentelemetry-demo/pull/2170))
* [chore] bump dependent images
  ([#2179](https://github.com/open-telemetry/opentelemetry-demo/pull/2179))
* [image-provider]: replace bookworm image with nonroot alpine image
  ([#2193](https://github.com/open-telemetry/opentelemetry-demo/pull/2193))
* [kafka] update image to latest
  ([#2194](https://github.com/open-telemetry/opentelemetry-demo/pull/2194))
* [email] bump ruby and dependencies to latest and switch to alpine
  ([#2196](https://github.com/open-telemetry/opentelemetry-demo/pull/2196))
* [cart] Upgrade OpenFeature version and change Hooks integration
  ([#2199](https://github.com/open-telemetry/opentelemetry-demo/pull/2199))
* [shipping] refactor service to use actix-web and demo instrumentation library
  ([#2214](https://github.com/open-telemetry/opentelemetry-demo/pull/2214))
* [quote] replace debian image with latest alpine image
  ([#2216](https://github.com/open-telemetry/opentelemetry-demo/pull/2216))
* [payment] change image from alpine to distroless to reduce size
  ([#2224](https://github.com/open-telemetry/opentelemetry-demo/pull/2224))
* [frontend] change image from alpine to distroless to reduce size
  ([#2224](https://github.com/open-telemetry/opentelemetry-demo/pull/2224))
* [flagd-ui] change image from alpine to distroless to reduce size
  ([#2224](https://github.com/open-telemetry/opentelemetry-demo/pull/2224))
* [load-generator] Update locustfile for logging with TraceContext
  ([#2265](https://github.com/open-telemetry/opentelemetry-demo/pull/2265))
* [product-catalog] Add OTel grpc Logs to Product Catalog
  ([#2285](https://github.com/open-telemetry/opentelemetry-demo/pull/2285))
* [currency] update alpine to 3.21
  ([#2291](https://github.com/open-telemetry/opentelemetry-demo/pull/2291))
* [shipping]: replace debian image with distroless image
  ([#2294](https://github.com/open-telemetry/opentelemetry-demo/pull/2294))
* [currency] update alpine to 3.21
  ([#2291](https://github.com/open-telemetry/opentelemetry-demo/pull/2291))
* [currency] Update code to use new semconv and remove unused file
  ([#2319](https://github.com/open-telemetry/opentelemetry-demo/pull/2319))
* [load-generator] Split trace grouping based on workflow context
  ([#2321](https://github.com/open-telemetry/opentelemetry-demo/pull/2321))
* [image-provider] Add nginx metrics receiver and dashboard
  ([#2330](https://github.com/open-telemetry/opentelemetry-demo/pull/2330))
* [react-native-app] Update how resource attributes are set up
  ([#2331](https://github.com/open-telemetry/opentelemetry-demo/pull/2331))
* [cart] Upgrade OpenFeature and add new telemetry Hooks
  ([#2332](https://github.com/open-telemetry/opentelemetry-demo/pull/2332))
* [otel-collector] Add support for OpenSearch dynamic index
  ([#2363](https://github.com/open-telemetry/opentelemetry-demo/pull/2363))
* [checkout] Add OTel grpc Logs to checkout
  ([#2336](https://github.com/open-telemetry/opentelemetry-demo/pull/2336))
* [prometheus] Activate `keep_identifying_resource_attributes` and promote
  Kubernetes resource attributes as metric labels
  ([#2340](https://github.com/open-telemetry/opentelemetry-demo/pull/2340))
* [grafana] Add APM dashboard including service metrics, traces, and logs
  ([#2340](https://github.com/open-telemetry/opentelemetry-demo/pull/2340))
* [payment] Send logs to otel-collector via pino-opentelemetry-transport
  ([#2352]((https://github.com/open-telemetry/opentelemetry-demo/pull/2352)))
* [image-provider] Update to latest version of nginx and alpine
  ([#2369](https://github.com/open-telemetry/opentelemetry-demo/pull/2369))
* [chore] Upgrade Jaeger to v2
  ([#2389](https://github.com/open-telemetry/opentelemetry-demo/pull/2389))
* [load-generator] Fix Playwright wait until load state error
  ([#2374](https://github.com/open-telemetry/opentelemetry-demo/pull/2374))
* [flagd] Bump Flagd to v0.12.8 and get compliant `http.Server.request.duration`
  OTel metrics that can be used in the APM dashboard
  ([#2392](https://github.com/open-telemetry/opentelemetry-demo/pull/2392))
* [prometheus / grafana] Add Linux monitoring dashboard
  ([#2395](https://github.com/open-telemetry/opentelemetry-demo/pull/2395))
* [prometheus /grafana] Add alerting demo through the `CartAddItemHighLatency`
  alert rule
  ([#2401](https://github.com/open-telemetry/opentelemetry-demo/pull/2401))
* [cart] Enable automatic generation of `service.instance.id`
  ([#2402](https://github.com/open-telemetry/opentelemetry-demo/pull/2402))
* [grafana] Update OpenSearch logs index in APM Dashboards
  ([#2419](https://github.com/open-telemetry/opentelemetry-demo/pull/2419))
* [flagd-ui] Rewrite Flagd UI in Elixir
  ([#2427](https://github.com/open-telemetry/opentelemetry-demo/pull/2427))
* [frontend-proxy] Add redirects for web UI paths to ensure proper asset loading
  ([#2476](https://github.com/open-telemetry/opentelemetry-demo/pull/2476))
* [chore] Bump dependent images
  ([#2477](https://github.com/open-telemetry/opentelemetry-demo/pull/2477))
* [email] Add memory leak scenario to email service
  ([#2481](https://github.com/open-telemetry/opentelemetry-demo/pull/2481))
* [checkout] Add graceful shutdown to checkout service
  ([#2491](https://github.com/open-telemetry/opentelemetry-demo/pull/2491))
* [shipping] Use cumulative metrics in shipping service to be consistent
  with the other services of the demo
  ([#2503](https://github.com/open-telemetry/opentelemetry-demo/pull/2503))
* [grafana] APM dashboard: Add host metrics per service instance
  ([#2507](https://github.com/open-telemetry/opentelemetry-demo/pull/2507))
* [react-native-app] Make frontend proxy URL configurable through app settings
  ([#2531](https://github.com/open-telemetry/opentelemetry-demo/pull/2531))

## 2.0.2

* [frontend] Update OpenTelemetry Browser SDK initialization
  ([#2092](https://github.com/open-telemetry/opentelemetry-demo/pull/2092))
* [quote] Updated open-telemetry/exporter-otlp to 1.2.1 which includes the
  fix for `IS_REMOTE` flag feature
  ([#2112](https://github.com/open-telemetry/opentelemetry-demo/pull/2112))
* [load-generator] Change OpenFeature Evaluation to Remote Evaluation Protocol,
  based on [this issue in OpenFeature/python-sdk-contrib](https://github.com/open-feature/python-sdk-contrib/issues/198)
  ([#2114](https://github.com/open-telemetry/opentelemetry-demo/pull/2114))
* [flagd-ui] increase memory to 100MB
  ([#2120](https://github.com/open-telemetry/opentelemetry-demo/pull/2120))
* [cartservice] change custom metrics to use seconds
  ([#2135](https://github.com/open-telemetry/opentelemetry-demo/pull/2135))
* [otel-collector] Fix OTel Collector meta-monitoring, export metrics using
  the HTTP port
  ([#2502](https://github.com/open-telemetry/opentelemetry-demo/pull/2502))

## 2.0.1

* [chore] Use Linkspector to check links
  ([#2070](https://github.com/open-telemetry/opentelemetry-demo/pull/2070))
* [frontend] Cypress tests base image updated to 14.0.3
  ([#2072](https://github.com/open-telemetry/opentelemetry-demo/pull/2072))
* [grafana] Update dashboards with service map
  ([#2085](https://github.com/open-telemetry/opentelemetry-demo/pull/2085))

## 2.0.0

* [grafana] Update grafana to 11.3.0
  ([#1764](https://github.com/open-telemetry/opentelemetry-demo/pull/1764))
* [chore] Move build args to .env file
  ([#1767](https://github.com/open-telemetry/opentelemetry-demo/pull/1767))
* [frontendproxy] add access logs
  ([#1768](https://github.com/open-telemetry/opentelemetry-demo/pull/1768))
* [grafana] Fix Dashboards
  ([#1779](https://github.com/open-telemetry/opentelemetry-demo/pull/1779))
* [accountingservice] bump OpenTelemetry .NET Automatic Instrumentation
  to 1.9.0 ([#1780](https://github.com/open-telemetry/opentelemetry-demo/pull/1780))
* [react-native-app] Add React Native example app
  ([#1781](https://github.com/open-telemetry/opentelemetry-demo/pull/1781))
* [chore] Add multi-platform build support
  ([#1785](https://github.com/open-telemetry/opentelemetry-demo/pull/1785))
* [chore] update memory limits for flagd, flagdui, and loadgenerator
  ([#1786](https://github.com/open-telemetry/opentelemetry-demo/pull/1786))
* [chore] Generate protobuf code for Go and Python services
  ([#1794](https://github.com/open-telemetry/opentelemetry-demo/pull/1784))
* [paymentservice] Add nodejs instrumentation for runtime metrics
  ([#1797](https://github.com/open-telemetry/opentelemetry-demo/pull/1797))
* [flagd and paymentservice] Update `paymentServiceFailure` to use a list of
  variants and add loyalty level attributes to spans. Added `service.name` to logs.
  ([#1815](https://github.com/open-telemetry/opentelemetry-demo/pull/1815))
* [accounting] rename accountingservice to accounting
  ([#1827](https://github.com/open-telemetry/opentelemetry-demo/pull/1827))
* [cartservice] - Add Exemplars to Cart Service
  ([#1830](https://github.com/open-telemetry/opentelemetry-demo/pull/1830))
* [ad] rename adservice to ad
  ([#1832](https://github.com/open-telemetry/opentelemetry-demo/pull/1832))
* [grafana] Add Exemplars Dashboard
  ([#1836](https://github.com/open-telemetry/opentelemetry-demo/pull/1836))
* [quote] rename quoteservice to quote
  ([#1838](https://github.com/open-telemetry/opentelemetry-demo/pull/1838))
* [cart] rename cartservice to cart
  ([#1839](https://github.com/open-telemetry/opentelemetry-demo/pull/1839))
* [flagd-ui] rename flagdui to flagd-ui
  ([#1840](https://github.com/open-telemetry/opentelemetry-demo/pull/1840))
* [otel-collector] rename otelcol to otel-collector
  ([#1841](https://github.com/open-telemetry/opentelemetry-demo/pull/1841))
* [shipping] rename shippingservice to shipping
  ([#1842](https://github.com/open-telemetry/opentelemetry-demo/pull/1842))
* [chore] Update demo Dependencies (Collector, Grafana, FlagD, Jaeger, Prometheus)
  ([#1855](https://github.com/open-telemetry/opentelemetry-demo/pull/1855))
* [load-generator] rename loadgenerator to load-generator
  ([#1856](https://github.com/open-telemetry/opentelemetry-demo/pull/1856))
* [image-provider] rename imageprovider to image-provider
  ([#1857](https://github.com/open-telemetry/opentelemetry-demo/pull/1857))
* [currency] rename currencyservice to currency
  ([#1858](https://github.com/open-telemetry/opentelemetry-demo/pull/1858))
* [email] rename emailservice to email
  ([#1861](https://github.com/open-telemetry/opentelemetry-demo/pull/1861))
* [fraud-detection] rename frauddetectionservice to fraud-detection
  ([#1862](https://github.com/open-telemetry/opentelemetry-demo/pull/1862))
* [payment] rename paymentservice to payment
  ([#1863](https://github.com/open-telemetry/opentelemetry-demo/pull/1863))
* [recommendation] rename recommendationservice to recommendation
  ([#1865](https://github.com/open-telemetry/opentelemetry-demo/pull/1865))
* [product-catalog] rename productcatalogservice to product-catalog
  ([#1864](https://github.com/open-telemetry/opentelemetry-demo/pull/1864))
* [checkout] rename checkoutservice to checkout
  ([#1867](https://github.com/open-telemetry/opentelemetry-demo/pull/1867))
* [chore] remove `SERVICE_` from environment variables
  ([#1897](https://github.com/open-telemetry/opentelemetry-demo/pull/1897))
* [frontend-proxy] rename frontendproxy to frontend-proxy
  ([#1910](https://github.com/open-telemetry/opentelemetry-demo/pull/1910))
* [product-catalog] load product list on a periodic timer
  ([#1919](https://github.com/open-telemetry/opentelemetry-demo/pull/1919))
* [flagd-ui] fixed eslint ignore comment with useCallback
  ([#1923](https://github.com/open-telemetry/opentelemetry-demo/pull/1923))
* [frontend-proxy] fix envoy access logs
  ([#1930](https://github.com/open-telemetry/opentelemetry-demo/pull/1930))
* [chore] Add memory for frontend-proxy, kafka, grafana, opensearch
  ([#1931](https://github.com/open-telemetry/opentelemetry-demo/pull/1931))
* [frontendproxy] fix Docker compose DNS resolver with envoy 1.32
  ([#1936](https://github.com/open-telemetry/opentelemetry-demo/pull/1936))
* [chore] Generate protobuf code for Typescript service - Frontend
  ([#1954](https://github.com/open-telemetry/opentelemetry-demo/pull/1954))
* [accounting] bump OpenTelemetry .NET Automatic Instrumentation to 1.10.0
  ([#1998](https://github.com/open-telemetry/opentelemetry-demo/pull/1998))
* [frontend] update to Node 22
  ([#2025](https://github.com/open-telemetry/opentelemetry-demo/pull/2025))
* [frontend] move page titles to individual pages
  ([#2025](https://github.com/open-telemetry/opentelemetry-demo/pull/2025))

## 1.12.0

* [accountingservice] allow running the container with non root user
  ([#1692](https://github.com/open-telemetry/opentelemetry-demo/pull/1692))
* [chore] Add yamllint to `make all`
  ([#1707](https://github.com/open-telemetry/opentelemetry-demo/pull/1707))
* [chore] Fix gen-proto for accountingservice
  ([#1709](https://github.com/open-telemetry/opentelemetry-demo/pull/1709))
* [chore] Add depends on to otelcol to wait on healthy opensearch
  ([#1724](https://github.com/open-telemetry/opentelemetry-demo/pull/1724))
* [flagd-ui] Add UI for managing Flagd feature flags
  ([#1725](https://github.com/open-telemetry/opentelemetry-demo/pull/1725))
* [accountingservice] bump OpenTelemetry .NET Automatic Instrumentation
  to 1.8.0 together with other dependencies
  ([#1727](https://github.com/open-telemetry/opentelemetry-demo/pull/1727))
* [frontend] fix imageSlowLoad headers not applied
  to 1.8.0 together with other dependencies
  ([#1733](https://github.com/open-telemetry/opentelemetry-demo/pull/1733))
* [cartservice] Propagate cartservice exceptions
  ([#1744](https://github.com/open-telemetry/opentelemetry-demo/pull/1744))
* [cartservice] Update cart service to fail when cartServiceFailure is enabled
  ([#1748](https://github.com/open-telemetry/opentelemetry-demo/pull/1748))

## 1.11.1

* [otel-col] Add docker stats receiver
  ([#1650](https://github.com/open-telemetry/opentelemetry-demo/pull/1650))
* [otel-col] strip high-cardinality segments of span names
  ([#1668](https://github.com/open-telemetry/opentelemetry-demo/pull/1668))
* [tests] run trace based tests concurrently
  ([#1659](https://github.com/open-telemetry/opentelemetry-demo/pull/1659))
* [otel-col] Set OTLP receiver endpoint to avoid breaking changes
  ([#1662](https://github.com/open-telemetry/opentelemetry-demo/pull/1662))
* [accountingservice] increase memory to 120MB
  ([#1666](https://github.com/open-telemetry/opentelemetry-demo/pull/1666))
* [frontend] Update nodejs to latest LTS and bump dependencies
  ([#1670](https://github.com/open-telemetry/opentelemetry-demo/pull/1670))
* [otel-col] Add host metrics receiver
  ([#1675](https://github.com/open-telemetry/opentelemetry-demo/pull/1675))
* [adservice] bump dependencies & gradle version
  ([#1681](https://github.com/open-telemetry/opentelemetry-demo/pull/1681))

## 1.11.0

* [accountingservice] convert from Go service to .NET service, uses
  OpenTelemetry .NET Automatic Instrumentation.
  ([#1538](https://github.com/open-telemetry/opentelemetry-demo/pull/1538))
* [frontend] fixed default flagd port for HTTPS connections
  ([#1609](https://github.com/open-telemetry/opentelemetry-demo/pull/1609))
* [cartservice] bump .NET package to 1.9.0 release
  ([#1610](https://github.com/open-telemetry/opentelemetry-demo/pull/1610))
* [Valkey] Replace Redis with Valkey
  ([#1619](https://github.com/open-telemetry/opentelemetry-demo/pull/1619))
* [recommendation] updated flag name to match flagd configuration
  ([#1634](https://github.com/open-telemetry/opentelemetry-demo/pull/1634))

## 1.10.0

* [frauddetectionservice] use span links when consuming from Kafka
  ([#1501](https://github.com/open-telemetry/opentelemetry-demo/pull/1501))
* [frontend] reunite trace from loadgenerator
  ([#1506](https://github.com/open-telemetry/opentelemetry-demo/pull/1506))
* [repo] add traceBasedTests image to published images
  ([#1507](https://github.com/open-telemetry/opentelemetry-demo/pull/1507))
* [quoteservice] add manual metric, export logs periodically
  ([#1519](https://github.com/open-telemetry/opentelemetry-demo/pull/1519))
* [flagd] export flagd traces to otel collector
  ([#1522](https://github.com/open-telemetry/opentelemetry-demo/pull/1522))
* [frontend] Pass down image optimization requests to imageprovider
  ([#1522](https://github.com/open-telemetry/opentelemetry-demo/pull/1522))
* [kafka] add kafkaQueueProblems feature flag
  ([#1528](https://github.com/open-telemetry/opentelemetry-demo/pull/1528))
* [otelcollector] Add `redisreceiver`
  ([#1537](https://github.com/open-telemetry/opentelemetry-demo/pull/1537))
* [traceBasedTests] update to v1.0.0
  ([#1551](https://github.com/open-telemetry/opentelemetry-demo/pull/1551))
* [flagd] update to 0.10.1 and set 50M memory limit
  ([#1554](https://github.com/open-telemetry/opentelemetry-demo/pull/1554))
* [loadgenerator] Configure feature flag evaluation tracing
  ([#1553](https://github.com/open-telemetry/opentelemetry-demo/pull/1553))
* [recommendationservice] Configure feature flag evaluation tracing
  ([#1553](https://github.com/open-telemetry/opentelemetry-demo/pull/1553))
* [loadgenerator] Fix feature flag hooks setter method
  ([#1556](https://github.com/open-telemetry/opentelemetry-demo/pull/1556))
* [frontend] Slowloading of images based on imageSlowLoad flag
  ([#1515](https://github.com/open-telemetry/opentelemetry-demo/pull/1486))
* [frontend] Fix imageloading issues on optimized images. bump next.js version
  ([#1571](https://github.com/open-telemetry/opentelemetry-demo/pull/1571))
* [cartservice] bump .NET package to 1.8.1 release
  ([#1514](https://github.com/open-telemetry/opentelemetry-demo/pull/1514),
   [#1580](https://github.com/open-telemetry/opentelemetry-demo/pull/1580))
* [kafka] Fix permission issue with the telemetry agent when running in docker compose
  ([#1574](https://github.com/open-telemetry/opentelemetry-demo/pull/1574))
* [flagd] Add flagd service to minimal docker compose deployment
  ([#1585](https://github.com/open-telemetry/opentelemetry-demo/pull/1585))
* [kafka] Increase memory and Java heap limits
  ([#1592](https://github.com/open-telemetry/opentelemetry-demo/pull/1592))
* chore: Add service version to OTEL_RESOURCE_ATTRIBUTES
  ([#1594](https://github.com/open-telemetry/opentelemetry-demo/pull/1594))
* [checkout] increase Kafka resiliency and observability
  ([#1590](https://github.com/open-telemetry/opentelemetry-demo/pull/1590))

## 1.9.0

* [chore] docker compose: add container name as tag attribute to container logs
* [featureflag] deprecate in favor of flagd
  ([#1338](https://github.com/open-telemetry/opentelemetry-demo/pull/1388))
* [checkoutservice] add producer interceptor for tracing
  ([#1400](https://github.com/open-telemetry/opentelemetry-demo/pull/1400))
* [chore] increase memory for Collector and Jaeger
  ([#1396](https://github.com/open-telemetry/opentelemetry-demo/pull/1396))
* [chore] fix Make targets for restart and redeploy
  ([#1397](https://github.com/open-telemetry/opentelemetry-demo/pull/1397))
* [chore] add nightly releases
  ([#1398](https://github.com/open-telemetry/opentelemetry-demo/pull/1398))
* [checkoutservice] add producer interceptor for tracing
  ([#1400](https://github.com/open-telemetry/opentelemetry-demo/pull/1400))
* [productcatalogservice] fix graceful shutdown issues
  ([#1402](https://github.com/open-telemetry/opentelemetry-demo/pull/1402))
* [chore] remove unused integration test
  ([#1406](https://github.com/open-telemetry/opentelemetry-demo/pull/1406))
* [CartService] - Add Host Detector
  ([#1415](https://github.com/open-telemetry/opentelemetry-demo/pull/1415))
* [chore] - add tests and odd profiles to make stop
  ([#1427](https://github.com/open-telemetry/opentelemetry-demo/pull/1427))
* [shippingservice] fix context propagation
  ([#1433](https://github.com/open-telemetry/opentelemetry-demo/pull/1433))
* [chore] - Update Telemetry Components
  ([#1440](https://github.com/open-telemetry/opentelemetry-demo/pull/1440))
* [loadgenerator] emit logs via OTLP
  ([#1446](https://github.com/open-telemetry/opentelemetry-demo/pull/1446))
* [frontend] reset quantity when new product selected
  ([#1447](https://github.com/open-telemetry/opentelemetry-demo/pull/1447))
* [paymentservice] add paymentServiceFailure feature flag
  ([#1449](https://github.com/open-telemetry/opentelemetry-demo/pull/1449))
* [checkoutservice] add paymentServiceUnreachable feature flag
  ([#1449](https://github.com/open-telemetry/opentelemetry-demo/pull/1449))
* [Frontend-proxy] Add restart policy to compose file
  ([#1448](https://github.com/open-telemetry/opentelemetry-demo/pull/1448))
* [cartservice] update .NET to .NET 8.0.3
  ([#1460](https://github.com/open-telemetry/opentelemetry-demo/pull/1460))
* [adservice] add adServiceManualGC feature flag
  ([#1463](https://github.com/open-telemetry/opentelemetry-demo/pull/1463))
* [frontendproxy] remove deprecated start_child_span option
  ([#1469](https://github.com/open-telemetry/opentelemetry-demo/pull/1469))
* [currency] fix metric name
  ([#1470](https://github.com/open-telemetry/opentelemetry-demo/pull/1470))
* [frontend] disable instrumentation-fs library
  ([#1473](https://github.com/open-telemetry/opentelemetry-demo/pull/1473))
* [Imageprovider] Create Nginx service to host images, add instrumentation to it
  ([#1462](https://github.com/open-telemetry/opentelemetry-demo/pull/1462))
* [loadgenerator] added loadgeneratorFloodHomepage flagd
  ([#1486](https://github.com/open-telemetry/opentelemetry-demo/pull/1486))
* [adservice] add adServiceHighCpu feature flag
  ([#1510](https://github.com/open-telemetry/opentelemetry-demo/pull/1510))

## 1.8.0

* [grafana] update grafana to 10.2.3
  ([#1332](https://github.com/open-telemetry/opentelemetry-demo/pull/1332))
* [frontendproxy] Enable envoy environment resource detector
  ([#1291](https://github.com/open-telemetry/opentelemetry-demo/pull/1291))
* [currencyservice] - add package name prefix to `rpc.service` attribute
  ([#1333](https://github.com/open-telemetry/opentelemetry-demo/pull/1333))
* [currency] fix metric exporter options
  ([#1335](https://github.com/open-telemetry/opentelemetry-demo/pull/1335))
* [ffspostgres] define and use demo specific postgres image
  ([#1338](https://github.com/open-telemetry/opentelemetry-demo/pull/1338))
* [loadgenerator, frontend] enable browser traffic in loadgenerator using playwright
  ([#1345](https://github.com/open-telemetry/opentelemetry-demo/pull/1345))
* [accountingservice] update wiki link
  ([#1346](https://github.com/open-telemetry/opentelemetry-demo/pull/1346))
* [checkoutservice] update wiki link
  ([#1346](https://github.com/open-telemetry/opentelemetry-demo/pull/1346))
* [productcatalogservice] update wiki link
  ([#1346](https://github.com/open-telemetry/opentelemetry-demo/pull/1346))
* [adservice] added group and anonymous read permission to
  opentelemetry-javaagent.jar
  ([#1348](https://github.com/open-telemetry/opentelemetry-demo/pull/1348))
* [frauddetectionservice] added group and anonymous read permission to
  opentelemetry-javaagent.jar
  ([#1348](https://github.com/open-telemetry/opentelemetry-demo/pull/1348))
* [adservice] Major version update for Java instrumentation, version 2.0.0
  ([#1352](https://github.com/open-telemetry/opentelemetry-demo/pull/1352))
* [frauddetectionservice] Major version update for Java instrumentation,
  version 2.0.0
  ([#1352](https://github.com/open-telemetry/opentelemetry-demo/pull/1352))
* [kafka] Major version update for Java instrumentation, version 2.0.0
  ([#1352](https://github.com/open-telemetry/opentelemetry-demo/pull/1352))
* Align env variables for OTLP ports
  ([#1353](https://github.com/open-telemetry/opentelemetry-demo/pull/1353))
* Update dependent services - Collector, Grafana, Jaeger, Prometheus, etc.
  ([#1354](https://github.com/open-telemetry/opentelemetry-demo/pull/1354))
* [OpenSearch] Use native OpenSearch exporter from Collector
  ([#1356](https://github.com/open-telemetry/opentelemetry-demo/pull/1356))
* Update GO SDKs & fix metrics config
  ([#1357](https://github.com/open-telemetry/opentelemetry-demo/pull/1357))
* Update Python SDKs
  ([#1358](https://github.com/open-telemetry/opentelemetry-demo/pull/1358))
* [loadgenerator] fix browser traffic enabled flag
  ([#1359](https://github.com/open-telemetry/opentelemetry-demo/pull/1359))
* [productcatalog] allow products to be extended
  ([#1363](https://github.com/open-telemetry/opentelemetry-demo/pull/1363))
* [tests] update trace based tests for semantic conventions
  ([#1377](https://github.com/open-telemetry/opentelemetry-demo/pull/1377))
* [currencyservice] Add OTLP logs
  ([#1378](https://github.com/open-telemetry/opentelemetry-demo/pull/1378))
* [cartservice] update .NET to .NET 8.0.2
  ([#1380](https://github.com/open-telemetry/opentelemetry-demo/pull/1380))

## 1.7.2

* [cartservice] update .NET package to 1.7.0 release
  ([#1326](https://github.com/open-telemetry/opentelemetry-demo/pull/1326))
* [loadgenerator and recommendationservice] Update python base image
  ([#1329](https://github.com/open-telemetry/opentelemetry-demo/pull/1329))

## 1.7.1

* [grafana] revert to 10.2.0
* [cartservice] disable config reload
  ([#1312](https://github.com/open-telemetry/opentelemetry-demo/pull/1312))
* [cartservice] fixed cartServiceFailure feature flag
  ([#1313](https://github.com/open-telemetry/opentelemetry-demo/pull/1313))
* [accountingservice] Update dependencies and semconv
* ([#1316](https://github.com/open-telemetry/opentelemetry-demo/pull/1316))
* [featureflagservice] Allow setting initial feature flag values
  ([#1319](https://github.com/open-telemetry/opentelemetry-demo/pull/1319))

## 1.7.0

* update PHP quoteservice to use 1.0.0
  ([#1236](https://github.com/open-telemetry/opentelemetry-demo/pull/1236))
* Add ability to do probabilistic A/B testing with feature flags
  ([#1237](https://github.com/open-telemetry/opentelemetry-demo/pull/1237))
* add env var for pinning trace-based test tool version
  ([#1239](https://github.com/open-telemetry/opentelemetry-demo/pull/1239))
* [cartservice] Add .NET memory, CPU, and thread metrics
  ([#1265](https://github.com/open-telemetry/opentelemetry-demo/pull/1265))
* [cartservice] update .NET to .NET 8.0
  ([#1272](https://github.com/open-telemetry/opentelemetry-demo/pull/1272))
* update loadgenerator dependencies and the base image
  ([#1274](https://github.com/open-telemetry/opentelemetry-demo/pull/1274))
* [currencyservice]: update opentelemetry-cpp to 1.12.0
  ([#1275](https://github.com/open-telemetry/opentelemetry-demo/pull/1275))
* [currencyservice] bring back multistage build
  ([#1276](https://github.com/open-telemetry/opentelemetry-demo/pull/1276))
* fix env var for pinning trace-based test tool version
  ([#1283](https://github.com/open-telemetry/opentelemetry-demo/pull/1283))
* [accountingservice] Add additional attributes to Kafka spans
  ([#1286](https://github.com/open-telemetry/opentelemetry-demo/pull/1286))
* [shippingservice] update Rust OTel libraries to 0.21
  ([#1287](https://github.com/open-telemetry/opentelemetry-demo/pull/1287))

## 1.6.0

* update PHP quoteservice to use RC1
  ([#1114](https://github.com/open-telemetry/opentelemetry-demo/pull/1114))
* [cartservice] update .NET package to 1.6.0 release
  ([#1115](https://github.com/open-telemetry/opentelemetry-demo/pull/1115))
* Set metric description to blank for rpc.server.duration and queueSize
  ([#1120](https://github.com/open-telemetry/opentelemetry-demo/pull/1120))
* slugify Grafana dashboard name
  ([#1121](https://github.com/open-telemetry/opentelemetry-demo/pull/1121))
* [kafka frauddetection adservice] update java agent versions
  ([#1132](https://github.com/open-telemetry/opentelemetry-demo/pull/1132))
* update dependent components to latest versions
  ([#1146](https://github.com/open-telemetry/opentelemetry-demo/pull/1146))
* [prometheus] Enabled support for the OTLP write receiver
  ([#1149](https://github.com/open-telemetry/opentelemetry-demo/pull/1149))
* [grafana] fix dashboard metric names and update settings
  ([#1150](https://github.com/open-telemetry/opentelemetry-demo/pull/1150))
* [otelcol] add httpcheck receiver for synthetic check of frontendproxy
  ([#1162](https://github.com/open-telemetry/opentelemetry-demo/pull/1162))
* pinning trace-based test tool version and adding files as volumes
  ([#1182](https://github.com/open-telemetry/opentelemetry-demo/pull/1182))
* [jaeger] fix Jager SPM / Monitor support
  ([#1174](https://github.com/open-telemetry/opentelemetry-demo/pull/1174))
* [otelcol] merge configuration files for base and observability configs
  ([#1173](https://github.com/open-telemetry/opentelemetry-demo/pull/1173))
* [frontendproxy] Fix service graph by enabling client spans in envoy proxy
  ([#1180](https://github.com/open-telemetry/opentelemetry-demo/pull/1180))
* [java-services] Update java, gradle and OTel agent versions
  ([#1183](https://github.com/open-telemetry/opentelemetry-demo/pull/1183))
* [opensearch] Add OpenSearch as an OTLP Logging backend
  ([#1151](https://github.com/open-telemetry/opentelemetry-demo/pull/1151))
* [opensearch] Add Grafana dashboard panels for OpenSearch log data
  ([#1193](https://github.com/open-telemetry/opentelemetry-demo/pull/1193))
* [go-sdk] Workaround: disable gRPC metrics in Go services
  ([#1205](https://github.com/open-telemetry/opentelemetry-demo/pull/1205))

## 1.5.0

* update trace-based tests to test stream events
  ([#1072](https://github.com/open-telemetry/opentelemetry-demo/pull/1072))
* Add cartServiceFailure feature flag triggering Cart Service errors
  ([#824](https://github.com/open-telemetry/opentelemetry-demo/pull/824))
* [paymentservice] update JS SDKs to 1.12.0/0.38.0
  ([#853](https://github.com/open-telemetry/opentelemetry-demo/pull/853))
* [frontend] update JS SDKs to 1.12.0/0.38.0
  ([#853](https://github.com/open-telemetry/opentelemetry-demo/pull/853))
* [chore] use `otel-demo` namespace for generated kubernetes manifests
  ([#848](https://github.com/open-telemetry/opentelemetry-demo/pull/848))
* [collector] update collector version to 0.76.1 and remove connectors feature gate.
  ([#857](https://github.com/open-telemetry/opentelemetry-demo/pull/857))
* [shippingservice] update rust version and dependencies
  ([#865](https://github.com/open-telemetry/opentelemetry-demo/pull/865))
* [load generator] Bump loagen dependencies
  ([#869](https://github.com/open-telemetry/opentelemetry-demo/pull/869))
* [grafana] fix demo dashboard to be compatible with spanmetrics connector
  ([#874](https://github.com/open-telemetry/opentelemetry-demo/pull/874))
* [quoteservice] enabling batch span processor metrics
  ([#878](https://github.com/open-telemetry/opentelemetry-demo/pull/878))
* [kafka] remove KRaft mode support workarounds
  ([#880](https://github.com/open-telemetry/opentelemetry-demo/pull/880))
* [currencyservice] Fix OTel C++ build and update OTel version to 1.9.0
  ([#886](https://github.com/open-telemetry/opentelemetry-demo/pull/886))
* [featureflagservice] Upgrade opentelemetry_ecto to 1.1.1
  ([#899](https://github.com/open-telemetry/opentelemetry-demo/pull/899))
* [currencyservice] Fix OTLP export to use default env vars
  ([#904](https://github.com/open-telemetry/opentelemetry-demo/pull/904))
* [featureflagservice] Bump OTP version to 26.0
  ([#903](https://github.com/open-telemetry/opentelemetry-demo/pull/903))
* Regenerate kubernetes manifest and add auto-generate comment
  ([#909](https://github.com/open-telemetry/opentelemetry-demo/pull/909))
* [loadgenerator] fix redirect on recommendations load
  ([#913](https://github.com/open-telemetry/opentelemetry-demo/pull/913))
* [loadgenerator] run load through frontend proxy (Envoy)
  ([#914](https://github.com/open-telemetry/opentelemetry-demo/pull/914))
* [cartservice] update .NET package to 1.5.0 release
  ([#935](https://github.com/open-telemetry/opentelemetry-demo/pull/935))
* [cartservice] update service to .NET 7
  ([#942](https://github.com/open-telemetry/opentelemetry-demo/pull/942))
* [tests] Add trace-based testing examples
  ([#877](https://github.com/open-telemetry/opentelemetry-demo/pull/877))
* Introduce minimal mode to run demo
  ([#872](https://github.com/open-telemetry/opentelemetry-demo/pull/872))
* [frontendproxy]Envoy expose a route for the collector to route frontend spans
  ([#938](https://github.com/open-telemetry/opentelemetry-demo/pull/938))
* [frontend] update JS SDKs to 1.15.0/0.41.0
  ([#853](https://github.com/open-telemetry/opentelemetry-demo/pull/853))
* [shippingservice] Update Rust dependencies and add TelemetryResourceDetector
  ([#972](https://github.com/open-telemetry/opentelemetry-demo/pull/972))
* Update frontendproxy's env for minimal
  ([#983](https://github.com/open-telemetry/opentelemetry-demo/pull/983))
* [FeatureFlagService] Update dependencies
  ([#992](https://github.com/open-telemetry/opentelemetry-demo/pull/992))
* [currencyService] Update OTel dependency
  ([#991](https://github.com/open-telemetry/opentelemetry-demo/pull/991))
* [LoadGenerator & RecommendatationService] update dependencies
  ([#988](https://github.com/open-telemetry/opentelemetry-demo/pull/988))
* [FraudDetectionService] Updated Kotlin version and OTel dependencies
  ([#987](https://github.com/open-telemetry/opentelemetry-demo/pull/987))
* [quoteservice] update php dependencies
  ([#1009](https://github.com/open-telemetry/opentelemetry-demo/pull/1009))
* [tests] Update trace-based tests run script
  ([#1018](https://github.com/open-telemetry/opentelemetry-demo/pull/1018))
* [PaymentService] Update node to LTS version and bump deps
  ([#1029](https://github.com/open-telemetry/opentelemetry-demo/pull/1029))
* [frontend] Update dependencies
  ([#1054](https://github.com/open-telemetry/opentelemetry-demo/pull/1054))
* [frontendproxy] Fix typo URL endpoint for FrontendProxy
  ([#1075](https://github.com/open-telemetry/opentelemetry-demo/pull/1075))
* [checkoutservice] Upgrade Shopify/sarama to IBM/sarama
  ([#1083](https://github.com/open-telemetry/opentelemetry-demo/pull/1083))
* [accountingservice] Upgrade Shopify/sarama to IBM/sarama
  ([#1083](https://github.com/open-telemetry/opentelemetry-demo/pull/1083))
* Update Telemetry Components
  ([#1085](https://github.com/open-telemetry/opentelemetry-demo/pull/1085))
* [cartservice] Support for logs
  ([#1086](https://github.com/open-telemetry/opentelemetry-demo/pull/1086))
* [TraceTests] Update span attributes to align with new IBM/sarama instrumentation
  ([#1096](https://github.com/open-telemetry/opentelemetry-demo/pull/1096))

## 1.4.0

* [cart] use 60m TTL for cart entries in redis
  ([#779](https://github.com/open-telemetry/opentelemetry-demo/pull/779))
* spanmetrics dashboard service&operation rates & latencies
  ([#787](https://github.com/open-telemetry/opentelemetry-demo/pull/787))
* Adds Kubernetes manifests for the demo
  ([#791](https://github.com/open-telemetry/opentelemetry-demo/pull/791))
* [bug] fixing quoteservice metrics exporting (PHP)
  ([#793](https://github.com/open-telemetry/opentelemetry-demo/pull/793))
* Added app.session.id attribute to frontend spans
  ([#795](https://github.com/open-telemetry/opentelemetry-demo/pull/795))
* Add logs for Ad service and Recommendation service
  ([#796](https://github.com/open-telemetry/opentelemetry-demo/pull/796))
* Opentelemetry Collector Data Flow Dashboard
  ([#797](https://github.com/open-telemetry/opentelemetry-demo/pull/797))
* Fixed shipping update in the frontend UI when number of products in cart
  changes
  ([#799](https://github.com/open-telemetry/opentelemetry-demo/pull/799))
* Update frontend JavaScript SDKs to: 1.10.1/0.36.x
  ([#805](https://github.com/open-telemetry/opentelemetry-demo/pull/805))
* Fix http.status_code on error in frontend
  ([#810](https://github.com/open-telemetry/opentelemetry-demo/pull/810))
* Fix bug in shipping calculation
  ([#814](https://github.com/open-telemetry/opentelemetry-demo/pull/814))
* Reduce Kafka mem allocation
  ([#798](https://github.com/open-telemetry/opentelemetry-demo/pull/798))
* Updated frontend web tracer to us batch processor
  ([#819](https://github.com/open-telemetry/opentelemetry-demo/pull/819))
* Moved env platform flag to the footer, changed it to free text
  ([#818](https://github.com/open-telemetry/opentelemetry-demo/pull/818))
* Update OTel Collector
  ([#822](https://github.com/open-telemetry/opentelemetry-demo/pull/822))
* Update OTel Collector to use spanmetrics connector instead of spanmetrics
  processors
  ([#829](https://github.com/open-telemetry/opentelemetry-demo/pull/829))

## 1.3.1

* [docs] Drop docs folder as step in migration to OTel website
  ([#729](https://github.com/open-telemetry/opentelemetry-demo/issues/729))
* rename proto package from hipstershop to oteldemo
  ([#740](https://github.com/open-telemetry/opentelemetry-demo/pull/740))
* Removed unnecessary code from Program.cs
  ([#754](https://github.com/open-telemetry/opentelemetry-demo/pull/754))
* feature flag service: update the dependency tls_certificate_check and bump to
  OTP-25 ([#756](https://github.com/open-telemetry/opentelemetry-demo/pull/756))
* Bump up OTEL Java Agent version to 1.23.0
  ([#757](https://github.com/open-telemetry/opentelemetry-demo/pull/757))
* Add counter metric to currency service (C++)
  ([#759](https://github.com/open-telemetry/opentelemetry-demo/issues/759))
* Use browserDetector to populate browser info to frontend-web telemetry
  ([#760](https://github.com/open-telemetry/opentelemetry-demo/pull/760))
* [chore] update for Mac M2 architecture
  ([#764](https://github.com/open-telemetry/opentelemetry-demo/pull/764))
* [chore] align memory limits with Helm chart
  ([#781](https://github.com/open-telemetry/opentelemetry-demo/pull/781))
* Use an async PHP runtime, bump versions to latest betas
  ([#823](https://github.com/open-telemetry/opentelemetry-demo/pull/823))

## 1.3.0

* Use `frontend-web` as service name for browser/web requests
([#628](https://github.com/open-telemetry/opentelemetry-demo/pull/628))
* Update `quoteservice` to use opentelemetry-php beta release
([#644](https://github.com/open-telemetry/opentelemetry-demo/pull/644))
* Add build for arm64 arch
([#644](https://github.com/open-telemetry/opentelemetry-demo/pull/657))
* Add synthetic attribute flag to front end instrumentation
([#631](https://github.com/open-telemetry/opentelemetry-demo/pull/631))
* Fix the total sum on the cart page
([#633](https://github.com/open-telemetry/opentelemetry-demo/pull/633))
* Add OTel java agent with JMX Metric Insights to kafka
([#654](https://github.com/open-telemetry/opentelemetry-demo/pull/654))
* Add resource detectors to payment service
([#651](https://github.com/open-telemetry/opentelemetry-demo/pull/651))
* Add resource detectors to frontend service
([#648](https://github.com/open-telemetry/opentelemetry-demo/pull/648))
* Add Jaeger-SPM-Config
([#655](https://github.com/open-telemetry/opentelemetry-demo/pull/655))
* Add healthcheck to featureflagservice
([#661](https://github.com/open-telemetry/opentelemetry-demo/pull/661)
* Add resource detectors to checkout service
([#662](https://github.com/open-telemetry/opentelemetry-demo/pull/662))
* Add resource detectors to cart service
([#663](https://github.com/open-telemetry/opentelemetry-demo/pull/663))
* Add `OTEL_RESOURCE_ATTRIBUTES` to docker compose setup
([#664](https://github.com/open-telemetry/opentelemetry-demo/pull/664))
* Update loadgenerator python base image and dependencies
([#669](https://github.com/open-telemetry/opentelemetry-demo/pull/669))
* Add basic metric support to productcatalog service
([#674](https://github.com/open-telemetry/opentelemetry-demo/pull/674))
* Add resource detectors to accounting service
([#676](https://github.com/open-telemetry/opentelemetry-demo/pull/676))
* Add resource detectors to product catalog service
([#677](https://github.com/open-telemetry/opentelemetry-demo/pull/677))
* Add custom metrics to ads service
([#678](https://github.com/open-telemetry/opentelemetry-demo/pull/678))
* Rebuild currency service Dockerfile with alpine
([#687](https://github.com/open-telemetry/opentelemetry-demo/pull/687))
* Remove grpc from loadgenerator
([#688](https://github.com/open-telemetry/opentelemetry-demo/pull/688))
* Update docker-compose services to restart unless stopped
([#690](https://github.com/open-telemetry/opentelemetry-demo/pull/690))
* Use different docker base images for frauddetection service
([#691](https://github.com/open-telemetry/opentelemetry-demo/pull/691))
* Fix payment service version to support temporality environment variable
([#693](https://github.com/open-telemetry/opentelemetry-demo/pull/693))
* Update recommendationservice python base image and dependencies
([#700](https://github.com/open-telemetry/opentelemetry-demo/pull/700))
* Add adServiceFailure feature flag triggering Ad Service errors
([#694](https://github.com/open-telemetry/opentelemetry-demo/pull/694))
* Reduce spans generated from quote service
([#702](https://github.com/open-telemetry/opentelemetry-demo/pull/702))
* Update emailservice Dockerfile to use alpine and multistage build
([#703](https://github.com/open-telemetry/opentelemetry-demo/pull/703))
* Update dockerfile for adservice to use different base images
([#705](https://github.com/open-telemetry/opentelemetry-demo/pull/705))
* Enable exemplar support in the metrics exporter, Prometheus, and Grafana
([#704](https://github.com/open-telemetry/opentelemetry-demo/pull/704))
* Add cross-compilation for shipping service
([#715](https://github.com/open-telemetry/opentelemetry-demo/issues/715))

## 1.2.0

* Change ZipCode data type from int to string
([#587](https://github.com/open-telemetry/opentelemetry-demo/pull/587))
* Pass product's `categories` as an input for the Ad service
([#600](https://github.com/open-telemetry/opentelemetry-demo/pull/600))
* Add HTTP client instrumentation to shippingservice
([#610](https://github.com/open-telemetry/opentelemetry-demo/pull/610))
* Added Kafka, accountingservice and frauddetectionservice for async workflows
([#512](https://github.com/open-telemetry/opentelemetry-demo/pull/457))
* Added support for non-root containers
([#615](https://github.com/open-telemetry/opentelemetry-demo/pull/615))
* Add tracing to Envoy (frontend-proxy)
([#613](https://github.com/open-telemetry/opentelemetry-demo/pull/613))
* Build Kafka image
([#617](https://github.com/open-telemetry/opentelemetry-demo/pull/617))

## v1.1.0

* Replaced PHP-CLI to PHP-Apache for a more realistic service
([#563](https://github.com/open-telemetry/opentelemetry-demo/pull/563))
* Optimize currencyservice build time with parallel build jobs
([#569](https://github.com/open-telemetry/opentelemetry-demo/pull/569))
* Optimize GitHub Builds and fix broken emulation of featureflag
([#536](https://github.com/open-telemetry/opentelemetry-demo/pull/536))
* Add basic metrics support for payment service
([#583](https://github.com/open-telemetry/opentelemetry-demo/pull/583))

## v1.0.0

* Add component owners for adservice Java app by @trask in
  ([519](https://github.com/open-telemetry/opentelemetry-demo/pull/519))
* Add gradle wrapper validation by @trask in
  ([518](https://github.com/open-telemetry/opentelemetry-demo/pull/518))
* fix currency bug by @cartersocha in
  ([522](https://github.com/open-telemetry/opentelemetry-demo/pull/522))
* Final Docs Review by @austinlparker in
  ([515](https://github.com/open-telemetry/opentelemetry-demo/pull/515))
* Front End -> Frontend by @austinlparker in
  ([537](https://github.com/open-telemetry/opentelemetry-demo/pull/537))
* [docs] kubernetes by @puckpuck in
  ([521](https://github.com/open-telemetry/opentelemetry-demo/pull/521))
* bump to v1.0 for release by @austinlparker in
  ([538](https://github.com/open-telemetry/opentelemetry-demo/pull/538))

## v0.7.0-beta

* Update shippingservice to add resource data to spans
([#504](https://github.com/open-telemetry/opentelemetry-demo/pull/504))
* Add Envoy as reverse proxy for all user-facing services
([#508](https://github.com/open-telemetry/opentelemetry-demo/pull/508))
* Envoy: Grafana, Load Generator, Jaeger exposed.
([#513](https://github.com/open-telemetry/opentelemetry-demo/pull/513))
* Added frontend instrumentation exporter custom url
([#512](https://github.com/open-telemetry/opentelemetry-demo/pull/512))

## v0.6.1-beta

* Set resource memory limits for all services
([#460](https://github.com/open-telemetry/opentelemetry-demo/pull/460))
* Added cache scenario to recommendation service
([#455](https://github.com/open-telemetry/opentelemetry-demo/pull/455))
* Update cartservice Dockerfile to support ARM64
([#439](https://github.com/open-telemetry/opentelemetry-demo/pull/439))

## v0.6.0-beta

* Added basic metrics support for recommendation service (Python)
([#416](https://github.com/open-telemetry/opentelemetry-demo/pull/416))
* Added metrics auto-instrumentation + minor metrics refactor for recommendation
 service (Python)
 [#432](https://github.com/open-telemetry/opentelemetry-demo/pull/432)
* Replaced the Jaeger exporter to the OTLP exporter in the OTel Collector
([#435](https://github.com/open-telemetry/opentelemetry-demo/pull/435))

## v0.5.0

* Add custom span and custom span attributes for Feature Flag Service
([#371](https://github.com/open-telemetry/opentelemetry-demo/pull/371))
* Change Cart Service to be async
([#372](https://github.com/open-telemetry/opentelemetry-demo/pull/372))
* Removed Postgres error on startup
([#378](https://github.com/open-telemetry/opentelemetry-demo/pull/378))
* Fixed traffic to Ad and Recommendation Service
([#379](https://github.com/open-telemetry/opentelemetry-demo/pull/379))
* Add dotnet runtime metrics to the Cart Service
([#393](https://github.com/open-telemetry/opentelemetry-demo/pull/393))
* Add dotnet instrumentation libraries to the Cart Service
([#394](https://github.com/open-telemetry/opentelemetry-demo/pull/394))
* Fixed Feature Flag Service error on start up
([#402](https://github.com/open-telemetry/opentelemetry-demo/pull/402))
* Update Checkout Service Go version to 1.19 once OTel Go Metrics require 1.18+
([#409](https://github.com/open-telemetry/opentelemetry-demo/pull/409))
* Added hero scenario metric to Checkout Service on cache leak
([#339](https://github.com/open-telemetry/opentelemetry-demo/pull/339))

## v0.4.0

* Add span events to shipping service
([#344](https://github.com/open-telemetry/opentelemetry-demo/pull/344))
* Add PHP quote service
([#345](https://github.com/open-telemetry/opentelemetry-demo/pull/345))
* Improve initial run time, without a build
([#362](https://github.com/open-telemetry/opentelemetry-demo/pull/362))

## v0.3.0

* Enhanced cart service attributes
([#183](https://github.com/open-telemetry/opentelemetry-demo/pull/183))
* Re-implemented currency service using C++
([#189](https://github.com/open-telemetry/opentelemetry-demo/pull/189))
* Simplified repo name and dropped the '-webstore' suffix in every place
([#225](https://github.com/open-telemetry/opentelemetry-demo/pull/225))
* Added end-to-end tests to each individual service
([#242](https://github.com/open-telemetry/opentelemetry-demo/pull/242))
* Added ability for repo forks to specify additional collector settings
([#246](https://github.com/open-telemetry/opentelemetry-demo/pull/246))
* Add metrics endpoint in adservice to send metrics from java agent
([#237](https://github.com/open-telemetry/opentelemetry-demo/pull/237))
* Support override java agent jar
([#244](https://github.com/open-telemetry/opentelemetry-demo/pull/244))
* Pulling java agent from the Java instrumentation releases instead.
([#253](https://github.com/open-telemetry/opentelemetry-demo/pull/253))
* Added explicit support for Kubernetes.
([#255](https://github.com/open-telemetry/opentelemetry-demo/pull/255))
* Added spanmetrics processor to otelcol
([#212](https://github.com/open-telemetry/opentelemetry-demo/pull/212))
* Added span attributes to shipping service
([#260](https://github.com/open-telemetry/opentelemetry-demo/pull/260))
* Added span attributes to currency service
([#265](https://github.com/open-telemetry/opentelemetry-demo/pull/265))
* Restricted network and port bindings
([#272](https://github.com/open-telemetry/opentelemetry-demo/pull/272))
* Feature Flag Service UI exposed on port 8081
([#273](https://github.com/open-telemetry/opentelemetry-demo/pull/273))
* Reimplemented Frontend app using [Next.js](https://nextjs.org/) Browser client
([#236](https://github.com/open-telemetry/opentelemetry-demo/pull/236))
* Remove set_currency from load generator
([#290](https://github.com/open-telemetry/opentelemetry-demo/pull/290))
* Added Frontend [Cypress](https://www.cypress.io/) E2E tests
([#298](https://github.com/open-telemetry/opentelemetry-demo/pull/298))
* Added baggage support in CurrencyService
([#281](https://github.com/open-telemetry/opentelemetry-demo/pull/281))
* Added error for a specific product based on a feature flag
([#245](https://github.com/open-telemetry/opentelemetry-demo/pull/245))
* Added Frontend Instrumentation
([#293](https://github.com/open-telemetry/opentelemetry-demo/pull/293))
* Add Feature Flags definitions
([#314](https://github.com/open-telemetry/opentelemetry-demo/pull/314))
* Enable Locust loadgen environment variable config options
([#316](https://github.com/open-telemetry/opentelemetry-demo/pull/316))
* Simplified and cleaned up ProductCatalogService
([#317](https://github.com/open-telemetry/opentelemetry-demo/pull/317))
* Updated Product Catalog to Match Astronomy Webstore
([#285](https://github.com/open-telemetry/opentelemetry-demo/pull/285))
* Add Span link for synthetic requests (from load generator)
([#332](https://github.com/open-telemetry/opentelemetry-demo/pull/332))
* Add `synthetic_request=true` baggage to load generator requests
([#331](https://github.com/open-telemetry/opentelemetry-demo/pull/331))

## v0.2.0

* Added feature flag service implementation
([#141](https://github.com/open-telemetry/opentelemetry-demo/pull/141))
* Added additional attributes to productcatalog service
([#143](https://github.com/open-telemetry/opentelemetry-demo/pull/143))
* Added manual instrumentation to ad service
([#150](https://github.com/open-telemetry/opentelemetry-demo/pull/150))
* Added manual instrumentation to email service
([#158](https://github.com/open-telemetry/opentelemetry-demo/pull/158))
* Added basic metric support and Prometheus storage
([#160](https://github.com/open-telemetry/opentelemetry-demo/pull/160))
* Added manual instrumentation to recommendation service
([#163](https://github.com/open-telemetry/opentelemetry-demo/pull/163))
* Added manual instrumentation to checkout service
([#164](https://github.com/open-telemetry/opentelemetry-demo/pull/164))
* Added Grafana service and enhanced metric experience
([#175](https://github.com/open-telemetry/opentelemetry-demo/pull/175))

## v0.1.0

* The initial code base is donated from a
[fork](https://github.com/julianocosta89/opentelemetry-microservices-demo) of
the [Google microservices
demo](https://github.com/GoogleCloudPlatform/microservices-demo) with express
knowledge of the owners. The pre-existing copyrights will remain. Any future
significant modifications will be credited to OpenTelemetry Authors.
* Added feature flag service protos
([#26](https://github.com/open-telemetry/opentelemetry-demo/pull/26))
* Added span attributes to frontend service
([#82](https://github.com/open-telemetry/opentelemetry-demo/pull/82))
* Rewrote shipping service in Rust
([#35](https://github.com/open-telemetry/opentelemetry-demo/issues/35))
