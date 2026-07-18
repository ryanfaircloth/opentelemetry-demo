// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

using Accounting;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Logging.Console;

var consoleLogLevel = Helpers.ParseLogLevel(Environment.GetEnvironmentVariable("LOG_LEVEL")) ?? LogLevel.Warning;

var host = Host.CreateDefaultBuilder(args)
    .ConfigureLogging(logging =>
    {
        // Console is for operators: JSON-structured, WARN+ only (level
        // configurable via LOG_LEVEL). The overall minimum level stays at
        // Information so any injected OTel log bridge still captures Info+.
        logging.ClearProviders();
        logging.AddJsonConsole();
        logging.AddFilter<ConsoleLoggerProvider>(level => level >= consoleLogLevel);
        logging.SetMinimumLevel(LogLevel.Information);
    })
    .ConfigureServices(services =>
    {
        services.AddHostedService<Consumer>();
    })
    .Build();

var startupLogger = host.Services.GetRequiredService<ILoggerFactory>().CreateLogger("Accounting.Startup");
startupLogger.LogInformation("Accounting service started");
Environment.GetEnvironmentVariables()
    .FilterRelevant()
    .OutputInOrder(startupLogger);

await host.RunAsync();
