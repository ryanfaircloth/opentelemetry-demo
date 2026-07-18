// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

#include <algorithm>
#include <cstdlib>
#include <iostream>

#include "opentelemetry/logs/provider.h"
#include "opentelemetry/sdk/logs/logger.h"
#include "opentelemetry/sdk/logs/logger_provider_factory.h"
#include "opentelemetry/sdk/logs/batch_log_record_processor_factory.h"
#include "opentelemetry/sdk/logs/simple_log_record_processor_factory.h"
#include "opentelemetry/sdk/logs/logger_context_factory.h"
#include "opentelemetry/exporters/otlp/otlp_http_log_record_exporter_factory.h"
#include "opentelemetry/exporters/ostream/log_record_exporter_factory.h"

using namespace std;
namespace nostd     = opentelemetry::nostd;
namespace otlp      = opentelemetry::exporter::otlp;
namespace logs      = opentelemetry::logs;
namespace logs_sdk  = opentelemetry::sdk::logs;

namespace
{
  // Two independent LoggerProviders rather than one shared processor list:
  // this SDK's LogRecordProcessor has no per-processor severity filter, so a
  // shared provider can't give console and OTLP different minimum levels.
  // Splitting into two providers lets call sites decide which severities go
  // to which sink (see consoleShouldLogInfo below).
  std::shared_ptr<logs::LoggerProvider> otlpLoggerProvider;
  std::shared_ptr<logs::LoggerProvider> consoleLoggerProvider;

  void initLogger() {
    otlp::OtlpHttpLogRecordExporterOptions loggerOptions;
    auto otlp_exporter  = otlp::OtlpHttpLogRecordExporterFactory::Create(loggerOptions);
    auto otlp_processor = logs_sdk::BatchLogRecordProcessorFactory::Create(std::move(otlp_exporter), {});
    // push_back into a named vector rather than a brace-init temporary:
    // std::vector<unique_ptr<T>>{std::move(x)} copy-constructs from the
    // initializer_list's const backing array, which fails to compile for
    // move-only types like LogRecordProcessor.
    std::vector<std::unique_ptr<logs_sdk::LogRecordProcessor>> otlp_processors;
    otlp_processors.push_back(std::move(otlp_processor));
    auto otlp_context = logs_sdk::LoggerContextFactory::Create(std::move(otlp_processors));
    otlpLoggerProvider = logs_sdk::LoggerProviderFactory::Create(std::move(otlp_context));
    opentelemetry::logs::Provider::SetLoggerProvider(otlpLoggerProvider);

    // Console exporter so logs are still visible when the OTel collector is
    // unreachable or misconfigured, in addition to the OTLP export above.
    auto console_exporter  = opentelemetry::exporter::logs::OStreamLogRecordExporterFactory::Create(std::cout);
    auto console_processor = logs_sdk::SimpleLogRecordProcessorFactory::Create(std::move(console_exporter));
    std::vector<std::unique_ptr<logs_sdk::LogRecordProcessor>> console_processors;
    console_processors.push_back(std::move(console_processor));
    auto console_context = logs_sdk::LoggerContextFactory::Create(std::move(console_processors));
    consoleLoggerProvider = logs_sdk::LoggerProviderFactory::Create(std::move(console_context));
  }

  nostd::shared_ptr<opentelemetry::logs::Logger> getLogger(std::string name){
    return otlpLoggerProvider->GetLogger(name + "_logger", name, OPENTELEMETRY_SDK_VERSION);
  }

  nostd::shared_ptr<opentelemetry::logs::Logger> getConsoleLogger(std::string name){
    return consoleLoggerProvider->GetLogger(name + "_console_logger", name, OPENTELEMETRY_SDK_VERSION);
  }

  // Console is WARN+ by default; Error calls always go there. LOG_LEVEL=INFO
  // or DEBUG additionally sends Info-level calls to console, for local
  // troubleshooting.
  bool consoleShouldLogInfo() {
    const char *level = std::getenv("LOG_LEVEL");
    if (level == nullptr) return false;
    std::string upper(level);
    std::transform(upper.begin(), upper.end(), upper.begin(), ::toupper);
    return upper == "INFO" || upper == "DEBUG";
  }
}
