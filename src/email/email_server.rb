# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

require "logger"
require "ostruct"
require "pony"
require "socket"
require "sinatra"
require "open_feature/sdk"
require "openfeature/flagd/provider"

require "opentelemetry/sdk"
require "opentelemetry-logs-sdk"
require "opentelemetry-metrics-sdk"
require "opentelemetry/exporter/otlp"
require "opentelemetry-exporter-otlp-logs"
require "opentelemetry-exporter-otlp-metrics"
require "opentelemetry/instrumentation/sinatra"

set :port, ENV["EMAIL_PORT"]
# Sinatra's classic app enables Rack::CommonLogger by default, which logs an
# access-log line for every request at a level that bypasses $console_logger
# entirely - disable it so console output is actually governed by LOG_LEVEL.
set :logging, false

# Plain stdlib logger so WARN/ERROR always reach the console, independent of
# whether the OTLP collector is reachable. Level configurable via LOG_LEVEL
# (this service demonstrates the "default, unstructured" logging tier).
$console_logger = Logger.new($stdout)
$console_logger.level = Logger.const_get((ENV["LOG_LEVEL"] || "WARN").upcase)

# OTel logger is created before OpenTelemetry::SDK.configure runs (needed to
# capture WARN/ERROR from the flagd retry loop below, which happens first) -
# emitting through the pre-configure no-op provider is harmless, it just
# drops those records until the real provider is active.
$logger = OpenTelemetry.logger_provider.logger(name: 'email')

# Logs both to the console (with backtrace, if any) and to the OTel logger
# (INFO+ regardless of the console's WARN+ floor), matching the standard's
# expectation that OTel exposes strictly more than console.
def log_warn_or_error(severity, message, exception: nil)
  console_message = exception ? "#{message}: #{exception.message}\n#{exception.backtrace&.join("\n")}" : message
  $console_logger.send(severity, console_message)

  attributes = exception ? { 'exception.message' => exception.message, 'exception.stacktrace' => exception.backtrace&.join("\n") } : {}
  $logger.on_emit(
    timestamp: Time.now,
    severity_text: severity.to_s.upcase,
    body: message,
    attributes: attributes,
  )
end

# A plain HTTP health endpoint on its own port, started immediately and
# independent of the flagd retry below: Sinatra's classic app doesn't start
# listening on EMAIL_PORT until this whole file finishes loading, so a
# probe against the main port would kill the pod while it's still
# legitimately waiting on a slow-starting flagd.
health_port = (ENV["EMAIL_HEALTH_PORT"] || "8081").to_i
Thread.new do
  # TCPServer.new(port) binds INADDR_ANY (IPv4-only), unreachable from
  # kubelet on IPv6-only pod networks - bind the IPv6 wildcard instead.
  server = TCPServer.new("::", health_port)
  loop do
    client = server.accept
    begin
      client.write("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
    rescue StandardError
      # ignore write errors from a client that disconnected early
    ensure
      client.close
    end
  end
end

# Initialize OpenFeature SDK with flagd provider, retrying with exponential
# backoff since flagd may not be up yet when this service starts.
flagd_client = OpenFeature::Flagd::Provider.build_client

retry_delay = 1
total_waited = 0
max_total_wait = 120

begin
  flagd_client.configure do |config|
    config.host = ENV.fetch("FLAGD_HOST", "localhost")
    config.port = ENV.fetch("FLAGD_PORT", 8013).to_i
    config.tls = ENV.fetch("FLAGD_TLS", "false") == "true"
  end
rescue StandardError => e
  if total_waited >= max_total_wait
    log_warn_or_error(:error, "Failed to configure flagd client after #{total_waited}s, giving up", exception: e)
    exit(1)
  end

  log_warn_or_error(:warn, "Failed to configure flagd client (retrying in #{retry_delay}s)", exception: e)
  sleep retry_delay
  total_waited += retry_delay
  retry_delay = [retry_delay * 2, 30].min
  retry
end

OpenFeature::SDK.configure do |config|
  config.set_provider(flagd_client)
end

OpenTelemetry::SDK.configure do |c|
  c.use "OpenTelemetry::Instrumentation::Sinatra"
end

otlp_metric_exporter = OpenTelemetry::Exporter::OTLP::Metrics::MetricsExporter.new
OpenTelemetry.meter_provider.add_metric_reader(otlp_metric_exporter)
meter = OpenTelemetry.meter_provider.meter("email")
$confirmation_counter = meter.create_counter("demo.notification.confirmations", unit: "1", description: "Counts the number of order confirmation emails sent")

get "/healthz" do
  status 200
end

post "/send_order_confirmation" do
  data = JSON.parse(request.body.read, object_class: OpenStruct)

  # get the current auto-instrumented span
  current_span = OpenTelemetry::Trace.current_span
  current_span.add_attributes({
    "demo.order.id" => data.order.order_id,
  })

  $confirmation_counter.add(1)
  send_email(data)

end

error do
  OpenTelemetry::Trace.current_span.record_exception(env['sinatra.error'])
  log_warn_or_error(:error, "Unhandled exception while processing request", exception: env['sinatra.error'])
end

def send_email(data)
  # create and start a manual span
  tracer = OpenTelemetry.tracer_provider.tracer('email')
  tracer.in_span("send_email") do |span|
    # Check if memory leak flag is enabled
    client = OpenFeature::SDK.build_client
    memory_leak_multiplier = client.fetch_number_value(flag_key: "emailMemoryLeak", default_value: 0)

    # To speed up the memory leak we create a long email body
    confirmation_content = erb(:confirmation, locals: { order: data.order })
    whitespace_length = [0, confirmation_content.length * (memory_leak_multiplier-1)].max

    Pony.mail(
      to:       data.email,
      from:     "noreply@example.com",
      subject:  "Your confirmation email",
      body:     confirmation_content + " " * whitespace_length,
      via:      :test
    )

    # If not clearing the deliveries, the emails will accumulate in the test mailer
    # We use this to create a memory leak.
    if memory_leak_multiplier < 1
      Mail::TestMailer.deliveries.clear
    end

    span.set_attribute("demo.order.id", data.order.order_id)
    $logger.on_emit(
      timestamp: Time.now,
      severity_text: 'INFO',
      body: 'Order confirmation email sent',
      attributes: { 'demo.order.id' => data.order.order_id },
      event_name: 'email.confirmation_sent',
    )
  end
  # manually created spans need to be ended
  # in Ruby, the method `in_span` ends it automatically
  # check out the OpenTelemetry Ruby docs at: 
  # https://opentelemetry.io/docs/instrumentation/ruby/manual/#creating-new-spans 
end
