// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { NextApiRequest, NextApiResponse } from 'next';
import proxyToBackend from '../../../utils/backendProxy';

const { IMAGE_PROVIDER_HOST = 'image-provider', IMAGE_PROVIDER_PORT = '8081' } = process.env;

const handler = (req: NextApiRequest, res: NextApiResponse) =>
  proxyToBackend(req, res, IMAGE_PROVIDER_HOST, Number(IMAGE_PROVIDER_PORT), '/images');

export const config = {
  api: {
    bodyParser: false,
    externalResolver: true,
  },
};

export default handler;
