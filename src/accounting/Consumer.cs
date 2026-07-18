// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

using Confluent.Kafka;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;
using Npgsql;
using Oteldemo;
using Microsoft.EntityFrameworkCore;
using System.Diagnostics;
using System.Threading;

namespace Accounting;

internal class DBContext : DbContext
{
    public DbSet<OrderEntity> Orders { get; set; }
    public DbSet<OrderItemEntity> CartItems { get; set; }
    public DbSet<ShippingEntity> Shipping { get; set; }

    protected override void OnConfiguring(DbContextOptionsBuilder optionsBuilder)
    {
        var connectionString = Environment.GetEnvironmentVariable("DB_CONNECTION_STRING");

        optionsBuilder.UseNpgsql(connectionString).UseSnakeCaseNamingConvention();
    }
}


internal class Consumer : BackgroundService
{
    private static readonly string TopicName = Environment.GetEnvironmentVariable("KAFKA_TOPIC") ?? "orders";
    private static readonly string GroupId = Environment.GetEnvironmentVariable("KAFKA_CONSUMER_GROUP") ?? "accounting";

    // Bounds how long the constructor retries a failing Kafka connection
    // before giving up. Mirrors product-catalog's initDatabase() retry budget.
    private static readonly TimeSpan KafkaConnectMaxWait = TimeSpan.FromMinutes(2);
    private static readonly TimeSpan KafkaConnectMaxBackoff = TimeSpan.FromSeconds(30);

    private readonly ILogger _logger;
    private readonly IConsumer<string, byte[]> _consumer;
    private readonly string? _dbConnectionString;
    private static readonly ActivitySource MyActivitySource = new("Accounting.Consumer");

    public Consumer(ILogger<Consumer> logger)
    {
        _logger = logger;

        var servers = Environment.GetEnvironmentVariable("KAFKA_ADDR")
            ?? throw new InvalidOperationException("The KAFKA_ADDR environment variable is not set.");

        Log.KafkaConnecting(_logger, servers);

        _consumer = ConnectWithRetry(servers, _logger);

        _dbConnectionString = Environment.GetEnvironmentVariable("DB_CONNECTION_STRING");
    }

    // Retries building the consumer and subscribing with exponential backoff:
    // a broker that isn't reachable yet at startup (e.g. Kafka still coming
    // up) shouldn't crash the host immediately. Config errors (e.g. a missing
    // KAFKA_ADDR) are thrown before this is reached and are not retried here.
    private static IConsumer<string, byte[]> ConnectWithRetry(string servers, ILogger logger)
    {
        var deadline = DateTime.UtcNow.Add(KafkaConnectMaxWait);
        var backoff = TimeSpan.FromSeconds(1);

        for (var attempt = 1; ; attempt++)
        {
            try
            {
                var consumer = BuildConsumer(servers, logger);
                consumer.Subscribe(TopicName);
                return consumer;
            }
            catch (KafkaException e)
            {
                if (DateTime.UtcNow >= deadline)
                {
                    Log.KafkaConnectFailed(logger, attempt, e);
                    Environment.Exit(1);
                    throw;
                }

                Log.KafkaConnectRetrying(logger, attempt, backoff, e);
                Thread.Sleep(backoff);
                backoff = TimeSpan.FromSeconds(Math.Min(backoff.TotalSeconds * 2, KafkaConnectMaxBackoff.TotalSeconds));
            }
        }
    }

    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        await Task.Yield();

        try
        {
            while (!stoppingToken.IsCancellationRequested)
            {
                try
                {
                    using var activity = MyActivitySource.StartActivity("order-consumed",  ActivityKind.Internal);
                    var consumeResult = _consumer.Consume(stoppingToken);
                    ProcessMessage(consumeResult.Message);
                }
                catch (ConsumeException e)
                {
                    Log.ConsumeError(_logger, e, e.Error.Reason);
                }
            }
        }
        catch (OperationCanceledException) when (stoppingToken.IsCancellationRequested)
        {
        }
        finally
        {
            Log.ConsumerClosing(_logger);
            _consumer.Close();
        }
    }

    private void ProcessMessage(Message<string, byte[]> message)
    {
        try
        {
            var order = OrderResult.Parser.ParseFrom(message.Value);
            Log.OrderReceivedMessage(_logger, order);

            if (_dbConnectionString == null)
            {
                return;
            }

            using var dbContext = new DBContext();
            var orderEntity = new OrderEntity
            {
                Id = order.OrderId
            };
            dbContext.Add(orderEntity);
            foreach (var item in order.Items)
            {
                var orderItem = new OrderItemEntity
                {
                    ItemCostCurrencyCode = item.Cost.CurrencyCode,
                    ItemCostUnits = item.Cost.Units,
                    ItemCostNanos = item.Cost.Nanos,
                    ProductId = item.Item.ProductId,
                    Quantity = item.Item.Quantity,
                    OrderId = order.OrderId
                };

                dbContext.Add(orderItem);
            }

            var shipping = new ShippingEntity
            {
                ShippingTrackingId = order.ShippingTrackingId,
                ShippingCostCurrencyCode = order.ShippingCost.CurrencyCode,
                ShippingCostUnits = order.ShippingCost.Units,
                ShippingCostNanos = order.ShippingCost.Nanos,
                StreetAddress = order.ShippingAddress.StreetAddress,
                City = order.ShippingAddress.City,
                State = order.ShippingAddress.State,
                Country = order.ShippingAddress.Country,
                ZipCode = order.ShippingAddress.ZipCode,
                OrderId = order.OrderId
            };
            dbContext.Add(shipping);
            dbContext.SaveChanges();
        }
        catch (DbUpdateException ex) when (ex.InnerException is PostgresException { SqlState: PostgresErrorCodes.UniqueViolation })
        {
            Log.DuplicateOrderSkipped(_logger);
        }
        catch (Exception ex) when (ex is Google.Protobuf.InvalidProtocolBufferException or DbUpdateException)
        {
            // Malformed message or a (non-duplicate) DB write failure: log and
            // skip this record rather than crashing the consumer loop. Any other
            // exception type is unexpected and should propagate.
            Log.OrderParsingFailed(_logger, ex);
        }
    }

    private static IConsumer<string, byte[]> BuildConsumer(string servers, ILogger logger)
    {
        var conf = new ConsumerConfig
        {
            GroupId = GroupId,
            BootstrapServers = servers,
            // https://github.com/confluentinc/confluent-kafka-dotnet/tree/07de95ed647af80a0db39ce6a8891a630423b952#basic-consumer-example
            AutoOffsetReset = AutoOffsetReset.Earliest,
            EnableAutoCommit = true
        };

        ApplySecurityConfig(conf);

        return new ConsumerBuilder<string, byte[]>(conf)
            .SetLogHandler((_, logMessage) => Log.KafkaClientLog(logger, logMessage.Facility, logMessage.Message))
            // Reports broker-level issues (e.g. all brokers down, DNS failures) that
            // librdkafka retries internally and does not throw as a ConsumeException.
            // A fatal client-level error means the client itself is unusable going
            // forward (librdkafka won't recover it internally) - exit so the pod
            // restarts instead of running on indefinitely in a broken state.
            .SetErrorHandler((_, error) =>
            {
                Log.KafkaClientError(logger, error.Reason, error.IsFatal);
                if (error.IsFatal)
                {
                    Environment.Exit(1);
                }
            })
            .Build();
    }

    // Configures SASL/TLS from the KAFKA_PROTOCOL, KAFKA_SASL_MECHANISM,
    // KAFKA_SASL_USERNAME, KAFKA_SASL_PASSWORD, and KAFKA_SSL_TRUSTSTORE_CRT
    // env vars, matching the KafkaAccess-operator secret contract
    // (https://github.com/strimzi/kafka-access-operator). Absent
    // KAFKA_PROTOCOL (or PLAINTEXT), the plaintext/no-auth behavior is unchanged.
    private static void ApplySecurityConfig(ClientConfig conf)
    {
        var protocol = Environment.GetEnvironmentVariable("KAFKA_PROTOCOL");
        if (string.IsNullOrEmpty(protocol) || protocol == "PLAINTEXT")
        {
            return;
        }

        conf.SecurityProtocol = protocol switch
        {
            "SSL" => SecurityProtocol.Ssl,
            "SASL_PLAINTEXT" => SecurityProtocol.SaslPlaintext,
            "SASL_SSL" => SecurityProtocol.SaslSsl,
            _ => throw new InvalidOperationException($"Unsupported KAFKA_PROTOCOL '{protocol}'.")
        };

        if (protocol.StartsWith("SASL_"))
        {
            conf.SaslMechanism = Environment.GetEnvironmentVariable("KAFKA_SASL_MECHANISM") switch
            {
                "SCRAM-SHA-512" => SaslMechanism.ScramSha512,
                "SCRAM-SHA-256" => SaslMechanism.ScramSha256,
                "PLAIN" or null or "" => SaslMechanism.Plain,
                var mechanism => throw new InvalidOperationException($"Unsupported KAFKA_SASL_MECHANISM '{mechanism}'.")
            };
            conf.SaslUsername = Environment.GetEnvironmentVariable("KAFKA_SASL_USERNAME");
            conf.SaslPassword = Environment.GetEnvironmentVariable("KAFKA_SASL_PASSWORD");
        }

        var caCert = Environment.GetEnvironmentVariable("KAFKA_SSL_TRUSTSTORE_CRT");
        if (!string.IsNullOrEmpty(caCert))
        {
            conf.SslCaPem = caCert;
        }
    }

    public override void Dispose()
    {
        _consumer?.Dispose();
        base.Dispose();
    }
}
