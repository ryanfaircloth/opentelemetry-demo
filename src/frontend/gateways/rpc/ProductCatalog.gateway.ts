// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials } from '@grpc/grpc-js';
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
    ).catch((error) => {
      Log.error('ProductCatalogGateway.getProduct failed', error);
      throw error;
    });
  },
});

export default ProductCatalogGateway();
