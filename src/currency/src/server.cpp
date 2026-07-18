// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

#include <cstdlib>
#include <cstring>
#include <iostream>
#include <math.h>
#include <thread>
#include <arpa/inet.h>
#include <csignal>
#include <sys/socket.h>
#include <unistd.h>
#include <demo.grpc.pb.h>

#include "opentelemetry/trace/context.h"
#include "opentelemetry/semconv/incubating/rpc_attributes.h"
#include "opentelemetry/trace/span_context_kv_iterable_view.h"
#include "opentelemetry/baggage/baggage.h"
#include "opentelemetry/nostd/string_view.h"
#include "opentelemetry/logs/event_id.h"
#include "logger_common.h"
#include "meter_common.h"
#include "tracer_common.h"

#include <grpcpp/grpcpp.h>
#include <grpcpp/server.h>
#include <grpcpp/server_builder.h>
#include <grpcpp/server_context.h>
#include <grpcpp/impl/codegen/string_ref.h>

using namespace std;
using namespace opentelemetry::baggage;
using namespace opentelemetry::trace;

using oteldemo::Empty;
using oteldemo::GetSupportedCurrenciesResponse;
using oteldemo::CurrencyConversionRequest;
using oteldemo::Money;

using grpc::Status;
using grpc::ServerContext;
using grpc::ServerBuilder;
using grpc::Server;

using Span            = Span;
using SpanContext     = SpanContext;
namespace context     = opentelemetry::context;
namespace metrics_api = opentelemetry::metrics;
namespace nostd       = opentelemetry::nostd;
namespace semconv     = opentelemetry::semconv;

using opentelemetry::logs::EventId;

namespace
{
  EventId eventName(nostd::string_view name) {
    // The OTLP exporter ignores the numeric EventId and exports only the event name.
    // Use 0 to satisfy the C++ API; revisit when the C++ SDK provides guidance.
    return EventId{0, name};
  }

  std::unordered_map<std::string, double> currency_conversion
  {
    {"EUR", 1.0},
    {"USD", 1.1305},
    {"JPY", 126.40},
    {"BGN", 1.9558},
    {"CZK", 25.592},
    {"DKK", 7.4609},
    {"GBP", 0.85970},
    {"HUF", 315.51},
    {"PLN", 4.2996},
    {"RON", 4.7463},
    {"SEK", 10.5375},
    {"CHF", 1.1360},
    {"ISK", 136.80},
    {"NOK", 9.8040},
    {"HRK", 7.4210},
    {"RUB", 74.4208},
    {"TRY", 6.1247},
    {"AUD", 1.6072},
    {"BRL", 4.2682},
    {"CAD", 1.5128},
    {"CNY", 7.5857},
    {"HKD", 8.8743},
    {"IDR", 15999.40},
    {"ILS", 4.0875},
    {"INR", 79.4320},
    {"KRW", 1275.05},
    {"MXN", 21.7999},
    {"MYR", 4.6289},
    {"NZD", 1.6679},
    {"PHP", 59.083},
    {"SGD", 1.5349},
    {"THB", 36.012},
    {"ZAR", 16.0583},
  };

  const char* version_env = std::getenv("VERSION");
  std::string version = version_env != nullptr ? version_env : "";
  std::string name{ "currency" };

  nostd::unique_ptr<metrics_api::Counter<uint64_t>> currency_counter;
  nostd::shared_ptr<opentelemetry::logs::Logger> logger;

class CurrencyService final : public oteldemo::CurrencyService::Service
{
  Status GetSupportedCurrencies(ServerContext* context,
  	const Empty* request,
  	GetSupportedCurrenciesResponse* response) override
  {
    StartSpanOptions options;
    options.kind = SpanKind::kServer;
    GrpcServerCarrier carrier(context);

    auto prop        = context::propagation::GlobalTextMapPropagator::GetGlobalPropagator();
    auto current_ctx = context::RuntimeContext::GetCurrent();
    auto new_context = prop->Extract(carrier, current_ctx);
    options.parent   = GetSpan(new_context)->GetContext();

    std::string span_name = "Currency/GetSupportedCurrencies";
    auto span =
        get_tracer("currency")->StartSpan(span_name,
                                      {{semconv::rpc::kRpcSystemName, semconv::rpc::RpcSystemNameValues::kGrpc},
                                       {semconv::rpc::kRpcMethod, "oteldemo.CurrencyService/GetSupportedCurrencies"},
                                       {semconv::rpc::kRpcResponseStatusCode, "0"}},
                                      options);
    auto scope = get_tracer("currency")->WithActiveSpan(span);

    span->AddEvent("Processing supported currencies request");

    for (auto &code : currency_conversion) {
      response->add_currency_codes(code.first);
    }

    span->AddEvent("Currencies fetched, response sent back");
    span->SetStatus(StatusCode::kOk);

    logger->Info(eventName("currency.get_supported_currencies"), "GetSupportedCurrencies successful");

    // Make sure to end your spans!
    span->End();
  	return Status::OK;
  }

  double getDouble(Money& money) {
    auto units = money.units();
    auto nanos = money.nanos();

    double decimal = 0.0;
    while (nanos != 0) {
      double t = (double)(nanos%10)/10;
      nanos = nanos/10;
      decimal = decimal/10 + t;
    }

    return double(units) + decimal;
  }

  void getUnitsAndNanos(Money& money, double value) {
    long unit = (long)value;
    double rem = value - unit;
    long nano = rem * pow(10, 9);
    money.set_units(unit);
    money.set_nanos(nano);
  }

  Status Convert(ServerContext* context,
  	const CurrencyConversionRequest* request,
  	Money* response) override
  {
    StartSpanOptions options;
    options.kind = SpanKind::kServer;
    GrpcServerCarrier carrier(context);

    auto prop        = context::propagation::GlobalTextMapPropagator::GetGlobalPropagator();
    auto current_ctx = context::RuntimeContext::GetCurrent();
    auto new_context = prop->Extract(carrier, current_ctx);
    options.parent   = GetSpan(new_context)->GetContext();

    std::string span_name = "Currency/Convert";
    auto span =
        get_tracer("currency")->StartSpan(span_name,
                                      {{semconv::rpc::kRpcSystemName, semconv::rpc::RpcSystemNameValues::kGrpc},
                                       {semconv::rpc::kRpcMethod, "oteldemo.CurrencyService/Convert"},
                                       {semconv::rpc::kRpcResponseStatusCode, "0"}},
                                      options);
    auto scope = get_tracer("currency")->WithActiveSpan(span);

    span->AddEvent("Processing currency conversion request");

    try {
      // Do the conversion work
      Money from = request->from();
      string from_code = from.currency_code();
      string to_code = request->to_code();

      if (currency_conversion.find(from_code) == currency_conversion.end()) {
        string msg = "unsupported currency code: " + from_code;
        span->AddEvent("Conversion failed");
        span->SetStatus(StatusCode::kError, msg);
        logger->Error(eventName("currency.conversion_failed"), msg.c_str());
        span->End();
        return Status(grpc::StatusCode::INVALID_ARGUMENT, msg);
      }
      if (currency_conversion.find(to_code) == currency_conversion.end()) {
        string msg = "unsupported currency code: " + to_code;
        span->AddEvent("Conversion failed");
        span->SetStatus(StatusCode::kError, msg);
        logger->Error(eventName("currency.conversion_failed"), msg.c_str());
        span->End();
        return Status(grpc::StatusCode::INVALID_ARGUMENT, msg);
      }

      double rate = currency_conversion.at(from_code);
      double one_euro = getDouble(from) / rate ;
      double to_rate = currency_conversion.at(to_code);

      double final = one_euro * to_rate;
      getUnitsAndNanos(*response, final);
      response->set_currency_code(to_code);

      span->SetAttribute("demo.exchange.from", from_code);
      span->SetAttribute("demo.exchange.to", to_code);

      CurrencyCounter(to_code);

      span->AddEvent("Conversion successful, response sent back");
      span->SetStatus(StatusCode::kOk);

      logger->Info(eventName("currency.conversion"),
                   "conversion successful",
                   opentelemetry::common::MakeAttributes(
                       {{"currency.from", from_code.c_str()},
                        {"currency.to", to_code.c_str()}}));
      
      // End the span
      span->End();
      return Status::OK;

    } catch(const std::exception& e) {
      span->AddEvent("Conversion failed");
      span->SetStatus(StatusCode::kError, e.what());

      logger->Error(eventName("currency.conversion_failed"), e.what());

      span->End();
      return Status::CANCELLED;

    } catch(...) {
      span->AddEvent("Conversion failed");
      span->SetStatus(StatusCode::kError);

      logger->Error(eventName("currency.conversion_failed"), "conversion failure");

      span->End();
      return Status::CANCELLED;
    }
    return Status::OK;
  }

  void CurrencyCounter(const std::string& to_code)
  {
      std::map<std::string, std::string> labels = { {"demo.exchange.to", to_code} };
      auto labelkv = common::KeyValueIterableView<decltype(labels)>{ labels };
      currency_counter->Add(1, labelkv);
  }
};

// RunHealthHttpServer serves a plain HTTP health endpoint on its own port,
// replacing the gRPC health service this service used to register: kubelet's
// httpGet probe needs no gRPC client tooling, and this avoids the
// fragile-precompiled-gencode class of problem gRPC health checking
// libraries can hit under auto-instrumentation injection (see the
// recommendation service's RECOMMENDATION_HEALTH_PORT for precedent). C++
// has no stdlib HTTP server, so this responds to any connection with a fixed
// 200 OK without parsing the request - sufficient for a liveness/readiness
// probe that just needs a 2xx status line.
void RunHealthHttpServer(uint16_t port)
{
  int server_fd = socket(AF_INET6, SOCK_STREAM, 0);
  if (server_fd < 0) {
    logger->Error(eventName("currency.health.socket_failed"), "failed to create health check socket");
    return;
  }

  int opt = 1;
  setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
  // IPV6_V6ONLY off (the Linux default) makes this socket dual-stack.
  int v6only = 0;
  setsockopt(server_fd, IPPROTO_IPV6, IPV6_V6ONLY, &v6only, sizeof(v6only));

  sockaddr_in6 addr{};
  addr.sin6_family = AF_INET6;
  addr.sin6_addr = in6addr_any;
  addr.sin6_port = htons(port);

  if (bind(server_fd, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) < 0 ||
      listen(server_fd, 16) < 0) {
    logger->Error(eventName("currency.health.listen_failed"), "failed to bind/listen health check socket");
    close(server_fd);
    return;
  }

  logger->Info(eventName("currency.health.started"), "Currency HTTP health endpoint started");

  static const char response[] =
      "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";

  while (true) {
    int client_fd = accept(server_fd, nullptr, nullptr);
    if (client_fd < 0) {
      continue;
    }
    // MSG_NOSIGNAL: a probe that closes its side early would otherwise raise
    // SIGPIPE, whose default disposition kills the whole process (not just
    // this thread).
    send(client_fd, response, sizeof(response) - 1, MSG_NOSIGNAL);
    close(client_fd);
  }
}

void RunServer(uint16_t port)
{
  // "[::]" binds dual-stack (IPv4 and IPv6) on Linux by default, since
  // IPV6_V6ONLY defaults to off; gRPC falls back to IPv4-only "0.0.0.0" if
  // the host has no IPv6 support at all.
  std::string address("[::]:" + std::to_string(port));

  CurrencyService currencyService;
  ServerBuilder builder;

  builder.RegisterService(&currencyService);
  builder.AddListeningPort(address, grpc::InsecureServerCredentials());

  std::unique_ptr<Server> server(builder.BuildAndStart());
  logger->Info(eventName("currency.server.started"),
               "Currency Server started",
               opentelemetry::common::MakeAttributes({{"server.address", address.c_str()}}));

  const char* health_port_env = std::getenv("CURRENCY_HEALTH_PORT");
  uint16_t health_port = health_port_env != nullptr ? static_cast<uint16_t>(atoi(health_port_env)) : 8081;
  std::thread health_thread(RunHealthHttpServer, health_port);
  health_thread.detach();

  server->Wait();
  server->Shutdown();
}
}

int main(int argc, char **argv) {

  // A client (e.g. a probe) closing its side of a socket before we finish
  // writing would otherwise raise SIGPIPE, whose default disposition kills
  // the whole process.
  signal(SIGPIPE, SIG_IGN);

  if (argc < 2) {
    std::cout << "Usage: currency <port>";
    return 0;
  }

  uint16_t port = atoi(argv[1]);

  initTracer();
  initMeter();
  initLogger();
  currency_counter = initIntCounter("demo.exchange.conversions", version);
  logger = getLogger(name);
  RunServer(port);

  return 0;
}
