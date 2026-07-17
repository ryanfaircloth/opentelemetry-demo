// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

using System.Threading.Tasks;
using System.Threading;

using OpenFeature;

using Microsoft.Extensions.Diagnostics.HealthChecks;

namespace cart.healthcheck
{
    public class readinessCheck : IHealthCheck
    {
        private readonly IFeatureClient _featureClient;

        public readinessCheck(IFeatureClient featureClient)
        {
            _featureClient = featureClient;
        }
        public async Task<HealthCheckResult> CheckHealthAsync(HealthCheckContext context, CancellationToken cancellationToken = default)
        {

            #pragma warning disable CA2016 // OpenFeature does not support CancellationToken
            // Await the async call instead of blocking
            bool isSet = await _featureClient.GetBooleanValueAsync("failedReadinessProbe", false); // Replace with actual check
            #pragma warning restore CA2016
            if (isSet)
            {
                return HealthCheckResult.Unhealthy("connection failed");

            }

            return HealthCheckResult.Healthy("healthy");
        }
    }
}
