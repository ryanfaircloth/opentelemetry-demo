// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiRequest, NextApiResponse } from 'next';
import http from 'http';

/*
 * Streams a browser request through to a backing service. The browser reaches
 * flagd, the collector's OTLP receiver, and image-provider on same-origin
 * paths (/flagservice, /otlp-http, /images); a gateway or the frontend-proxy
 * may route those paths straight to the backends, but when neither is in
 * front of us the frontend serves them itself, so a deployment needs no
 * routing wiring at all for the app to fully work. Mirrors the
 * frontend-proxy's prefix_rewrite: "/" semantics: the first path segment is
 * stripped before forwarding, and responses are piped unbuffered so flagd's
 * grpc-web event stream keeps flowing.
 */
const proxyToBackend = (req: NextApiRequest, res: NextApiResponse, host: string, port: number) => {
  const segments = req.query.path;
  const path = '/' + (Array.isArray(segments) ? segments.join('/') : segments || '');
  const queryIndex = req.url?.indexOf('?') ?? -1;
  const search = queryIndex >= 0 ? req.url?.slice(queryIndex) : '';

  const upstream = http.request(
    {
      host,
      port,
      path: `${path}${search}`,
      method: req.method,
      headers: { ...req.headers, host: `${host}:${port}` },
    },
    backendRes => {
      res.writeHead(backendRes.statusCode || 502, backendRes.headers);
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
