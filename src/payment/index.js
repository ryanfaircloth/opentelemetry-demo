// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
const grpc = require('@grpc/grpc-js')
const protoLoader = require('@grpc/proto-loader')
const http = require('http')
const opentelemetry = require('@opentelemetry/api')

const charge = require('./charge')
const logger = require('./logger')

async function chargeServiceHandler(call, callback) {
  const span = opentelemetry.trace.getActiveSpan();

  try {
    const amount = call.request.amount
    span?.setAttributes({
      'demo.payment.amount': parseFloat(`${amount.units}.${amount.nanos}`).toFixed(2)
    })
    logger.info("Charge request received.")

    const response = await charge.charge(call.request)
    callback(null, response)

  } catch (err) {
    logger.warn({ err })

    span?.setStatus({ code: opentelemetry.SpanStatusCode.ERROR, message: err.message })
    callback(err)
  }
}

async function closeGracefully(signal) {
  server.forceShutdown()
  process.kill(process.pid, signal)
}

const otelDemoPackage = grpc.loadPackageDefinition(protoLoader.loadSync('demo.proto'))
const server = new grpc.Server()

server.addService(otelDemoPackage.oteldemo.PaymentService.service, { charge: chargeServiceHandler })


let ip = "0.0.0.0";

const ipv6_enabled = process.env.IPV6_ENABLED;

if (ipv6_enabled == "true") {
  ip = "[::]";
  logger.info(`Overwriting Localhost IP: ${ip}`)
}

const address = ip + `:${process.env['PAYMENT_PORT']}`;

server.bindAsync(address, grpc.ServerCredentials.createInsecure(), (err, port) => {
  if (err) {
    logger.error({ err })
    process.exit(1)
  }

  logger.info(`payment gRPC server started on ${address}`)
})

// A plain HTTP health endpoint: kubelet's httpGet probe needs no gRPC
// client tooling and avoids the fragile-precompiled-gencode class of
// problem gRPC health checking libraries can hit under
// auto-instrumentation injection (see the recommendation service's
// RECOMMENDATION_HEALTH_PORT for precedent). Replaces the gRPC health
// service this service used to register.
const healthPort = process.env.PAYMENT_HEALTH_PORT || 8081
const healthServer = http.createServer((req, res) => {
  if (req.url === '/healthz') {
    res.writeHead(200)
    res.end()
    return
  }
  res.writeHead(404)
  res.end()
})
healthServer.listen(healthPort, () => {
  logger.info(`payment health check HTTP server started on port ${healthPort}`)
})

process.once('SIGINT', closeGracefully)
process.once('SIGTERM', closeGracefully)
