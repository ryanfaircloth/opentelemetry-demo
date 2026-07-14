// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials } from '@grpc/grpc-js';
import { Cart, CartItem, CartServiceClient, Empty } from '../../protos/demo';
import Log from '../../utils/Log';

const { CART_ADDR = '' } = process.env;

const client = new CartServiceClient(CART_ADDR, ChannelCredentials.createInsecure());

const CartGateway = () => ({
  getCart(userId: string) {
    return new Promise<Cart>((resolve, reject) =>
      client.getCart({ userId }, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CartGateway.getCart failed', error);
      throw error;
    });
  },
  addItem(userId: string, item: CartItem) {
    return new Promise<Empty>((resolve, reject) =>
      client.addItem({ userId, item }, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CartGateway.addItem failed', error);
      throw error;
    });
  },
  emptyCart(userId: string) {
    return new Promise<Empty>((resolve, reject) =>
      client.emptyCart({ userId }, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CartGateway.emptyCart failed', error);
      throw error;
    });
  },
});

export default CartGateway();
