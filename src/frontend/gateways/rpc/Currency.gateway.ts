// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials } from '@grpc/grpc-js';
import { GetSupportedCurrenciesResponse, CurrencyServiceClient, Money } from '../../protos/demo';
import Log from '../../utils/Log';

const { CURRENCY_ADDR = '' } = process.env;

const client = new CurrencyServiceClient(CURRENCY_ADDR, ChannelCredentials.createInsecure());

const CurrencyGateway = () => ({
  convert(from: Money, toCode: string) {
    return new Promise<Money>((resolve, reject) =>
      client.convert({ from, toCode }, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CurrencyGateway.convert failed', error);
      throw error;
    });
  },
  getSupportedCurrencies() {
    return new Promise<GetSupportedCurrenciesResponse>((resolve, reject) =>
      client.getSupportedCurrencies({}, (error, response) => (error ? reject(error) : resolve(response)))
    ).catch((error) => {
      Log.error('CurrencyGateway.getSupportedCurrencies failed', error);
      throw error;
    });
  },
});

export default CurrencyGateway();
