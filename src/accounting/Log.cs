// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

using Microsoft.Extensions.Logging;
using Oteldemo;

namespace Accounting
{
    internal static partial class Log
    {
        [LoggerMessage(
            Level = LogLevel.Information,
            EventName = "accounting.order.received",
            Message = "Order details: {@OrderResult}.")]
        public static partial void OrderReceivedMessage(ILogger logger, OrderResult orderResult);

        [LoggerMessage(
            Level = LogLevel.Information,
            EventName = "accounting.kafka.connecting",
            Message = "Connecting to Kafka: {servers}")]
        public static partial void KafkaConnecting(ILogger logger, string servers);

        [LoggerMessage(
            Level = LogLevel.Debug,
            EventName = "accounting.kafka.client_log",
            Message = "Kafka client [{facility}]: {message}")]
        public static partial void KafkaClientLog(ILogger logger, string facility, string message);

        [LoggerMessage(
            Level = LogLevel.Warning,
            EventName = "accounting.kafka.connect_retrying",
            Message = "Kafka not ready yet (attempt {attempt}), retrying in {backoff}")]
        public static partial void KafkaConnectRetrying(ILogger logger, int attempt, TimeSpan backoff, Exception exception);

        [LoggerMessage(
            Level = LogLevel.Error,
            EventName = "accounting.kafka.connect_failed",
            Message = "Failed to connect to Kafka after {attempt} attempts, giving up.")]
        public static partial void KafkaConnectFailed(ILogger logger, int attempt, Exception exception);

        [LoggerMessage(
            Level = LogLevel.Warning,
            EventName = "accounting.kafka.client_error",
            Message = "Kafka client error (fatal={isFatal}): {reason}")]
        public static partial void KafkaClientError(ILogger logger, string reason, bool isFatal);

        [LoggerMessage(
            Level = LogLevel.Error,
            EventName = "accounting.kafka.consume_failed",
            Message = "Consume error: {reason}")]
        public static partial void ConsumeError(ILogger logger, Exception exception, string reason);

        [LoggerMessage(
            Level = LogLevel.Information,
            EventName = "accounting.consumer.closing",
            Message = "Closing consumer")]
        public static partial void ConsumerClosing(ILogger logger);

        [LoggerMessage(
            Level = LogLevel.Information,
            EventName = "accounting.order.duplicate_skipped",
            Message = "Duplicate order received, skipping.")]
        public static partial void DuplicateOrderSkipped(ILogger logger);

        [LoggerMessage(
            Level = LogLevel.Error,
            EventName = "accounting.order.parsing_failed",
            Message = "Order parsing failed:")]
        public static partial void OrderParsingFailed(ILogger logger, Exception exception);
    }
}
