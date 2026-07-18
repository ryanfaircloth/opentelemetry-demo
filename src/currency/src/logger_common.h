// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

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
  void initLogger() {
    otlp::OtlpHttpLogRecordExporterOptions loggerOptions;
    auto otlp_exporter  = otlp::OtlpHttpLogRecordExporterFactory::Create(loggerOptions);
    auto otlp_processor = logs_sdk::BatchLogRecordProcessorFactory::Create(std::move(otlp_exporter), {});

    // Console exporter so logs are still visible when the OTel collector is
    // unreachable or misconfigured, in addition to the OTLP export above.
    auto console_exporter  = opentelemetry::exporter::logs::OStreamLogRecordExporterFactory::Create(std::cout);
    auto console_processor = logs_sdk::SimpleLogRecordProcessorFactory::Create(std::move(console_exporter));

    std::vector<std::unique_ptr<logs_sdk::LogRecordProcessor>> processors;
    processors.push_back(std::move(otlp_processor));
    processors.push_back(std::move(console_processor));
    auto context = logs_sdk::LoggerContextFactory::Create(std::move(processors));
    std::shared_ptr<logs::LoggerProvider> provider = logs_sdk::LoggerProviderFactory::Create(std::move(context));
    opentelemetry::logs::Provider::SetLoggerProvider(provider);
  }

  nostd::shared_ptr<opentelemetry::logs::Logger> getLogger(std::string name){
    auto provider = logs::Provider::GetLoggerProvider();
    return provider->GetLogger(name + "_logger", name, OPENTELEMETRY_SDK_VERSION);
  }
}
