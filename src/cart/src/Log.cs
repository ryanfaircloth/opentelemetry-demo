// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

using System;
using Microsoft.Extensions.Logging;

namespace cart;

internal static partial class Log
{
    [LoggerMessage(Level = LogLevel.Debug, EventName = "cart.redis.connecting", Message = "Connecting to Redis: {connectionString}")]
    public static partial void RedisConnecting(ILogger logger, string connectionString);

    [LoggerMessage(Level = LogLevel.Error, EventName = "cart.redis.connection_failed", Message = "Wasn't able to connect to redis")]
    public static partial void RedisConnectionFailed(ILogger logger);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.redis.connected", Message = "Successfully connected to Redis")]
    public static partial void RedisConnected(ILogger logger);

    [LoggerMessage(Level = LogLevel.Debug, EventName = "cart.redis.small_test", Message = "Performing small test")]
    public static partial void RedisSmallTest(ILogger logger);

    [LoggerMessage(Level = LogLevel.Debug, EventName = "cart.redis.small_test_result", Message = "Small test result: {result}")]
    public static partial void RedisSmallTestResult(ILogger logger, string result);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.redis.connection_restored", Message = "Connection to redis was restored successfully.")]
    public static partial void RedisConnectionRestored(ILogger logger);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.redis.connection_lost", Message = "Connection failed. Disposing the object")]
    public static partial void RedisConnectionLost(ILogger logger);

    [LoggerMessage(Level = LogLevel.Error, EventName = "cart.redis.internal_error", Message = "Redis internal error")]
    public static partial void RedisInternalError(ILogger logger, Exception exception);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.add_item", Message = "AddItemAsync called with userId={userId}, productId={productId}, quantity={quantity}")]
    public static partial void AddItemAsync(ILogger logger, string userId, string productId, int quantity);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.empty", Message = "EmptyCartAsync called with userId={userId}")]
    public static partial void EmptyCartAsync(ILogger logger, string userId);

    [LoggerMessage(Level = LogLevel.Information, EventName = "cart.get", Message = "GetCartAsync called with userId={userId}")]
    public static partial void GetCartAsync(ILogger logger, string userId);

    [LoggerMessage(Level = LogLevel.Critical, EventName = "cart.startup.missing_valkey_addr", Message = "VALKEY_ADDR environment variable is required.")]
    public static partial void MissingValkeyAddr(ILogger logger);

    [LoggerMessage(Level = LogLevel.Warning, EventName = "cart.startup.retry", Message = "Failed to initialize cart store, retrying in {delaySeconds}s")]
    public static partial void CartStoreInitializationRetry(ILogger logger, double delaySeconds, Exception exception);

    [LoggerMessage(Level = LogLevel.Error, EventName = "cart.startup.failed", Message = "Failed to initialize cart store within the startup retry budget; exiting")]
    public static partial void CartStoreInitializationFailed(ILogger logger, Exception exception);

    [LoggerMessage(Level = LogLevel.Error, EventName = "cart.redis.operation_failed", Message = "Redis operation failed for userId={userId}")]
    public static partial void RedisOperationFailed(ILogger logger, string userId, Exception exception);

    [LoggerMessage(Level = LogLevel.Warning, EventName = "cart.redis.ping_failed", Message = "Redis ping failed")]
    public static partial void RedisPingFailed(ILogger logger, Exception exception);

    [LoggerMessage(Level = LogLevel.Warning, EventName = "cart.grpc.rpc_failed", Message = "{method} failed")]
    public static partial void RpcFailed(ILogger logger, string method, Exception exception);

    public static LogLevel? ParseLogLevel(string? value) => value?.ToUpperInvariant() switch
    {
        "DEBUG" => LogLevel.Debug,
        "INFO" or "INFORMATION" => LogLevel.Information,
        "WARN" or "WARNING" => LogLevel.Warning,
        "ERROR" => LogLevel.Error,
        _ => null,
    };
}
