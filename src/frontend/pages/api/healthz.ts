// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import type { NextApiRequest, NextApiResponse } from 'next';
import InstrumentationMiddleware from '../../utils/telemetry/InstrumentationMiddleware';

const handler = (_req: NextApiRequest, res: NextApiResponse) => {
  return res.status(200).end();
};

export default InstrumentationMiddleware(handler);
