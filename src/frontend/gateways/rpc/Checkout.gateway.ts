// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials } from '@grpc/grpc-js';
import { CheckoutServiceClient, PlaceOrderRequest, PlaceOrderResponse } from '../../protos/demo';
import Log from '../../utils/Log';

const { CHECKOUT_ADDR = '' } = process.env;

const client = new CheckoutServiceClient(CHECKOUT_ADDR, ChannelCredentials.createInsecure());

const CheckoutGateway = () => ({
  placeOrder(order: PlaceOrderRequest) {
    return new Promise<PlaceOrderResponse>((resolve, reject) =>
      client.placeOrder(order, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CheckoutGateway.placeOrder failed', error);
      throw error;
    });
  },
});

export default CheckoutGateway();
