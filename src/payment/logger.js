// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

const pino = require('pino')

// Console (JSON, this service demonstrates the "modern structured" logging
// tier) and OTLP export are two separate targets with independent minimum
// levels: console defaults to WARN (configurable via LOG_LEVEL), OTLP
// always gets Info+. A single shared record processor previously fed both
// at the same (unset, i.e. Info) level.
const consoleLevel = (process.env.LOG_LEVEL || 'warn').toLowerCase()

const transport = pino.transport({
  targets: [
    {
      target: 'pino-opentelemetry-transport',
      level: 'info',
      options: {
        logRecordProcessorOptions: [
          {
            recordProcessorType: 'batch',
            exporterOptions: {
              protocol: 'grpc',
            }
          }
        ],
        loggerName: 'payment-logger',
        serviceVersion: '1.0.0'
      }
    },
    {
      target: 'pino/file',
      level: consoleLevel,
      options: { destination: 1 } // stdout, JSON
    }
  ]
})

const logger = pino(transport, {
  level: 'info',
  mixin() {
    return {
      'service.name': process.env['OTEL_SERVICE_NAME'],
    }
  },
  formatters: {
    level: (label) => {
      return { 'level': label };
    },
  },
});

module.exports = logger;
