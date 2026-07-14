// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

const warn = (message: string, ...meta: unknown[]) => {
  console.warn(message, ...meta);
};

const error = (message: string, ...meta: unknown[]) => {
  console.error(message, ...meta);
};

const Log = { warn, error };

export default Log;
