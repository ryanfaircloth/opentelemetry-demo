// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials, status as GrpcStatus, ServiceError } from '@grpc/grpc-js';
import { ListProductsResponse, Product, ProductCatalogServiceClient } from '../../protos/demo';
import Log from '../../utils/Log';

const { PRODUCT_CATALOG_ADDR = '' } = process.env;

const client = new ProductCatalogServiceClient(PRODUCT_CATALOG_ADDR, ChannelCredentials.createInsecure());

const ProductCatalogGateway = () => ({
  listProducts() {
    return new Promise<ListProductsResponse>((resolve, reject) =>
      client.listProducts({}, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('ProductCatalogGateway.listProducts failed', error);
      throw error;
    });
  },
  getProduct(id: string) {
    return new Promise<Product>((resolve, reject) =>
      client.getProduct({ id }, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error: ServiceError) => {
      // A client looking up a product ID that doesn't exist is an expected
      // outcome of a well-formed request, not an application error - don't
      // log it as one (that's the same log-noise/alerting-fatigue problem
      // as marking an expected "not found" span as an error).
      if (error.code !== GrpcStatus.NOT_FOUND) {
        Log.error('ProductCatalogGateway.getProduct failed', error);
      }
      throw error;
    });
  },
});

export default ProductCatalogGateway();
