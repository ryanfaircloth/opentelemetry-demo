// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiRequest, NextApiResponse } from 'next';
import http from 'http';

/*
 * Streams a browser request through to a backing service. The browser reaches
 * flagd, the collector's OTLP receiver, and image-provider on same-origin
 * paths (/flagservice, /otlp-http, /images); a gateway may route those paths
 * straight to the backends, but when none does the frontend serves them
 * itself, so a deployment needs no routing wiring at all for the app to
 * fully work. targetPrefix is prepended to the forwarded path: backends that
 * serve their public path natively (image-provider's /images) get it back,
 * while backends that serve at their root (the collector's /v1/traces,
 * flagd's grpc-web services) get the public prefix stripped. Responses are
 * piped unbuffered so flagd's grpc-web event stream keeps flowing.
 */
// Hop-by-hop headers (RFC 9110 §7.6.1) describe a single connection and must
// not be forwarded: Node frames each leg itself, and copying the backend's
// transfer-encoding while piping the already-decoded body corrupts streaming
// responses (flagd's EventStream never parses). accept-encoding is dropped
// too so backends answer with identity encoding - these bodies are framed
// protocol data or already-compressed images, and an encoded stream would
// have to be decoded here to stay parseable.
const HOP_BY_HOP_HEADERS = [
  'accept-encoding',
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
];

const withoutHopByHopHeaders = (headers: http.IncomingHttpHeaders) => {
  const result = { ...headers };
  for (const name of HOP_BY_HOP_HEADERS) {
    delete result[name];
  }
  return result;
};

const proxyToBackend = (
  req: NextApiRequest,
  res: NextApiResponse,
  host: string,
  port: number,
  targetPrefix = ''
) => {
  const segments = req.query.path;
  const path = targetPrefix + '/' + (Array.isArray(segments) ? segments.join('/') : segments || '');
  const queryIndex = req.url?.indexOf('?') ?? -1;
  const search = queryIndex >= 0 ? req.url?.slice(queryIndex) : '';

  const upstream = http.request(
    {
      host,
      port,
      path: `${path}${search}`,
      method: req.method,
      headers: { ...withoutHopByHopHeaders(req.headers), host: `${host}:${port}` },
    },
    backendRes => {
      const headers = withoutHopByHopHeaders(backendRes.headers);
      // no-transform keeps Next's compression middleware (and any cache in
      // front) from re-encoding the body: compressing a flagd event stream
      // buffers it, so the browser never sees an event until the stream ends.
      headers['cache-control'] = [backendRes.headers['cache-control'], 'no-transform']
        .filter(Boolean)
        .join(', ');
      res.writeHead(backendRes.statusCode || 502, headers);
      backendRes.pipe(res);
    }
  );
  upstream.on('error', () => {
    if (!res.headersSent) {
      res.statusCode = 502;
    }
    res.end();
  });
  req.pipe(upstream);
};

export default proxyToBackend;
