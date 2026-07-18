// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import type { NextApiRequest, NextApiResponse } from 'next';
import { status as GrpcStatus, ServiceError } from '@grpc/grpc-js';
import InstrumentationMiddleware from '../../../../utils/telemetry/InstrumentationMiddleware';
import { Empty, Product } from '../../../../protos/demo';
import ProductCatalogService from '../../../../services/ProductCatalog.service';

type TResponse = Product | Empty | { error: string };

const handler = async ({ method, query }: NextApiRequest, res: NextApiResponse<TResponse>) => {
  switch (method) {
    case 'GET': {
      const { productId = '', currencyCode = '' } = query;
      try {
        const product = await ProductCatalogService.getProduct(productId as string, currencyCode as string);

        return res.status(200).json(product);
      } catch (error) {
        // A missing product ID is an expected outcome, not an application
        // error - return a real 404 instead of letting it fall through to
        // InstrumentationMiddleware's generic 500 path.
        if ((error as ServiceError).code === GrpcStatus.NOT_FOUND) {
          return res.status(404).json({ error: 'Product not found' });
        }
        throw error;
      }
    }

    default: {
      return res.status(405).send('');
    }
  }
};

export default InstrumentationMiddleware(handler);
