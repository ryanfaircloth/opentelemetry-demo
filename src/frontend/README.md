# Frontend service

The frontend is a [Next.js](https://nextjs.org/) application that is composed
by two layers.

1. Client side application. Which renders the components for the OTEL webstore.
2. API layer. Connects the client to the backend services by exposing REST endpoints.

## Build Locally

By running `docker compose up` at the root of the project you'll have access to
the frontend client by going to <http://localhost:8080/>.

## Local development

Currently, the easiest way to run the frontend for local development is to execute

```shell
docker compose run --service-ports -e NODE_ENV=development --volume $(pwd)/src/frontend:/app --volume $(pwd)/pb:/app/pb --user node --entrypoint sh frontend
```

from the root folder.

It will start all of the required backend services
and within the container simply run `npm run dev`.
After that the app should be available at <http://localhost:8080/>.

## Browser telemetry

The client bundle is instrumented independently of the API layer's own
OpenTelemetry SDK. `utils/telemetry/FrontendTracer.ts` sets up a
`WebTracerProvider` (`@opentelemetry/sdk-trace-web`) with
`getWebAutoInstrumentations` (document-load, fetch, XHR, user-interaction)
and a `BatchSpanProcessor` exporting via OTLP/HTTP
(`NEXT_PUBLIC_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`); it's registered from
`pages/_app.tsx`. W3C trace-context and baggage propagators keep browser
spans correlated with backend traces, and `SessionIdProcessor.ts` tags every
span with `session.id`/`enduser.id`.

To avoid CORS, the browser doesn't call the collector cross-origin directly:
`pages/_document.tsx` points the exporter at a same-origin path
(`/otlp-http/v1/traces`), which the reverse proxy in front of the app forwards
to the collector's OTLP/HTTP receiver.
