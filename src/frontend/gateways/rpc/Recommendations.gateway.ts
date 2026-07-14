// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { ChannelCredentials } from '@grpc/grpc-js';
import { ListRecommendationsResponse, RecommendationServiceClient } from '../../protos/demo';
import Log from '../../utils/Log';

const { RECOMMENDATION_ADDR = '' } = process.env;

const client = new RecommendationServiceClient(RECOMMENDATION_ADDR, ChannelCredentials.createInsecure());

const RecommendationsGateway = () => ({
  listRecommendations(userId: string, productIds: string[]) {
    return new Promise<ListRecommendationsResponse>((resolve, reject) =>
      client.listRecommendations({ userId, productIds }, (error, response) =>
        error ? reject(error) : resolve(response)
      )
    ).catch((error) => {
      Log.error('RecommendationsGateway.listRecommendations failed', error);
      throw error;
    });
  },
});

export default RecommendationsGateway();
