// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiHandler } from 'next';
import {context, Exception, Span, SpanStatusCode, trace} from '@opentelemetry/api';
import { SemanticAttributes } from '@opentelemetry/semantic-conventions';
import Log from '../Log';

const InstrumentationMiddleware = (handler: NextApiHandler): NextApiHandler => {
  return async (request, response) => {
    // No active span is possible if instrumentation hasn't initialized yet
    // for this request - don't force-cast away the undefined case, since
    // that would make span.recordException/setStatus/setAttribute below
    // throw from inside the catch/finally meant to guarantee a safe response.
    const span = trace.getSpan(context.active());

    let httpStatus = 200;
    try {
      await runWithSpan(span, async () => handler(request, response));
      httpStatus = response.statusCode;
    } catch (error) {
      span?.recordException(error as Exception);
      span?.setStatus({ code: SpanStatusCode.ERROR });
      httpStatus = 500;
      Log.error(`API route ${request.url} failed`, error);
      if (!response.headersSent) {
        response.status(500).json({ error: 'Internal server error' });
      }
    } finally {
      span?.setAttribute(SemanticAttributes.HTTP_STATUS_CODE, httpStatus);
    }
  };
};

async function runWithSpan(parentSpan: Span | undefined, fn: () => Promise<unknown>) {
  if (!parentSpan) {
    return await fn();
  }
  const ctx = trace.setSpan(context.active(), parentSpan);
  return await context.with(ctx, fn);
}

export default InstrumentationMiddleware;
