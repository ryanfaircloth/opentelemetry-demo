// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
using System;

using Microsoft.AspNetCore.Diagnostics.HealthChecks;
using System.Threading.Tasks;
using System.Threading;

using cart.cartstore;
using cart.services;
using cart.healthcheck;

using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Diagnostics.HealthChecks;
using Microsoft.Extensions.Logging;
using OpenFeature;
using OpenFeature.Hooks;
using OpenFeature.Providers.Flagd;

var builder = WebApplication.CreateBuilder(args);

// gRPC requires a pure HTTP/2 endpoint. Kestrel's EndpointDefaults config
// (appsettings.json / Kestrel__EndpointDefaults__*) only applies to endpoints
// declared under the Kestrel:Endpoints section, not to the endpoint Kestrel
// derives from ASPNETCORE_URLS - so it silently falls back to HTTP/1.1
// without TLS, breaking gRPC. Give the health check its own HTTP/1.1 port
// instead of trying to share the gRPC port.
int cartPort = int.Parse(builder.Configuration["CART_PORT"] ?? "8080");
int healthPort = int.Parse(builder.Configuration["CART_HEALTH_PORT"] ?? "8081");
builder.WebHost.ConfigureKestrel(options =>
{
    options.ListenAnyIP(cartPort, listenOptions => listenOptions.Protocols = HttpProtocols.Http2);
    options.ListenAnyIP(healthPort, listenOptions => listenOptions.Protocols = HttpProtocols.Http1);
});

string valkeyAddress = builder.Configuration["VALKEY_ADDR"];
if (string.IsNullOrEmpty(valkeyAddress))
{
    using var bootstrapLoggerFactory = LoggerFactory.Create(logging => logging.AddConsole());
    var bootstrapLogger = bootstrapLoggerFactory.CreateLogger("cart.Startup");
    cart.Log.MissingValkeyAddr(bootstrapLogger);
    Environment.Exit(1);
}

// Console is for operators: default plain-text formatter (this service
// demonstrates the "default, unstructured" logging tier), WARN+ only via
// LOG_LEVEL (default WARN). Overall minimum level stays Information so the
// injected .NET auto-instrumentation's log bridge still captures Info+.
var cartConsoleLevel = cart.Log.ParseLogLevel(Environment.GetEnvironmentVariable("LOG_LEVEL")) ?? LogLevel.Warning;
builder.Logging.AddConsole();
builder.Logging.AddFilter<Microsoft.Extensions.Logging.Console.ConsoleLoggerProvider>(level => level >= cartConsoleLevel);
builder.Logging.SetMinimumLevel(LogLevel.Information);

builder.Services.AddSingleton<ICartStore>(x =>
{
    var store = new ValkeyCartStore(x.GetRequiredService<ILogger<ValkeyCartStore>>(), valkeyAddress);
    var startupLogger = x.GetRequiredService<ILogger<Program>>();

    var delay = TimeSpan.FromSeconds(1);
    var maxDelay = TimeSpan.FromSeconds(30);
    var deadline = DateTime.UtcNow + TimeSpan.FromMinutes(2);
    while (true)
    {
        try
        {
            store.Initialize();
            break;
        }
        catch (Exception ex)
        {
            if (DateTime.UtcNow >= deadline)
            {
                cart.Log.CartStoreInitializationFailed(startupLogger, ex);
                Environment.Exit(1);
            }

            cart.Log.CartStoreInitializationRetry(startupLogger, delay.TotalSeconds, ex);
            Thread.Sleep(delay);
            delay = TimeSpan.FromSeconds(Math.Min(delay.TotalSeconds * 2, maxDelay.TotalSeconds));
        }
    }

    return store;
});

builder.Services.AddOpenFeature(openFeatureBuilder =>
{
    openFeatureBuilder
        .AddProvider(_ => new FlagdProvider())
        .AddHook<MetricsHook>()
        .AddHook<TraceEnricherHook>();
});

builder.Services.AddSingleton(x =>
    new CartService(
        x.GetRequiredService<ICartStore>(),
        new ValkeyCartStore(x.GetRequiredService<ILogger<ValkeyCartStore>>(), "badhost:1234"),
        x.GetRequiredService<IFeatureClient>(),
        x.GetRequiredService<ILogger<CartService>>()
));


builder.Services.AddGrpc();
builder.Services.AddSingleton<readinessCheck>();
builder.Services.AddHealthChecks()
    .AddCheck<readinessCheck>("oteldemo.CartService");

var app = builder.Build();

app.MapGrpcService<CartService>();

// A plain HTTP health endpoint, reusing the same readinessCheck registration
// that used to back this service's gRPC health check: kubelet's httpGet
// probe needs no gRPC client tooling and avoids the
// fragile-precompiled-gencode class of problem gRPC health checking
// libraries can hit under auto-instrumentation injection (see the
// recommendation service's RECOMMENDATION_HEALTH_PORT for precedent). Served
// on its own HTTP/1.1-only port (see ConfigureKestrel above) rather than the
// gRPC port.
app.MapHealthChecks("/healthz");

app.MapGet("/", async context =>
{
    await context.Response.WriteAsync("Communication with gRPC endpoints must be made through a gRPC client. To learn how to create a client, visit: https://go.microsoft.com/fwlink/?linkid=2086909");
});

app.Run();


