// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiRequest, NextApiResponse } from 'next';
import proxyToBackend from '../../../utils/backendProxy';

const { OTEL_COLLECTOR_HOST = 'otel-collector', OTEL_COLLECTOR_PORT_HTTP = '4318' } = process.env;

const handler = (req: NextApiRequest, res: NextApiResponse) =>
  proxyToBackend(req, res, OTEL_COLLECTOR_HOST, Number(OTEL_COLLECTOR_PORT_HTTP));

export const config = {
  api: {
    bodyParser: false,
    externalResolver: true,
  },
};

export default handler;
