// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiRequest, NextApiResponse } from 'next';
import proxyToBackend from '../../../utils/backendProxy';

const { FLAGD_HOST = 'flagd', FLAGD_PORT = '8013' } = process.env;

const handler = (req: NextApiRequest, res: NextApiResponse) =>
  proxyToBackend(req, res, FLAGD_HOST, Number(FLAGD_PORT));

export const config = {
  api: {
    bodyParser: false,
    externalResolver: true,
  },
};

export default handler;
