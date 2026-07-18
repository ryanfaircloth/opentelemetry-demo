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

// pino's signature is pino(options, destination) - options first. This was
// previously called as pino(transport, {...}), which silently discards the
// whole options object (pino sees a stream-like first argument and treats
// the second as unused): level/mixin/formatters below never took effect,
// service.name was missing from every log record, and LOG_LEVEL couldn't
// actually raise verbosity below pino's default 'info' root level.
const logger = pino({
  // pino's root level is a hard gate: a call below it never reaches any
  // transport regardless of that transport's own level. Set to the lowest
  // level any target could plausibly use (LOG_LEVEL is operator-controlled)
  // so each target's own `level` above does the real filtering.
  level: 'trace',
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
}, transport);

module.exports = logger;
