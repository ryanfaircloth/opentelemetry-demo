// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

const LEVELS = { warn: 30, error: 40, silent: 100 } as const;

const configuredLevel = (): number => {
  const raw = (process.env.LOG_LEVEL ?? 'warn').toLowerCase();
  return LEVELS[raw as keyof typeof LEVELS] ?? LEVELS.warn;
};

const warn = (message: string, ...meta: unknown[]) => {
  if (configuredLevel() <= LEVELS.warn) console.warn(message, ...meta);
};

const error = (message: string, ...meta: unknown[]) => {
  if (configuredLevel() <= LEVELS.error) console.error(message, ...meta);
};

const Log = { warn, error };

export default Log;
