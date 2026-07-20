// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

/** @type {import('next').NextConfig} */

const dotEnv = require('dotenv');
const dotenvExpand = require('dotenv-expand');
const { resolve } = require('path');

const myEnv = dotEnv.config({
  path: resolve(__dirname, '../../.env'),
});
dotenvExpand.expand(myEnv);

const {
  AD_ADDR = '',
  CART_ADDR = '',
  CHECKOUT_ADDR = '',
  CURRENCY_ADDR = '',
  PRODUCT_CATALOG_ADDR = '',
  RECOMMENDATION_ADDR = '',
  SHIPPING_ADDR = '',
  LOG_LEVEL = 'warn',
  ENV_PLATFORM = '',
  OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = '',
  OTEL_SERVICE_NAME = 'frontend',
  PUBLIC_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = '',
} = process.env;

const nextConfig = {
  reactStrictMode: true,
  output: 'standalone',
  // Same-origin browser paths for flagd, the collector's OTLP receiver, and
  // image-provider. A gateway/frontend-proxy in front of us may route these
  // straight to the backends; when none does, the requests fall through to
  // these API-route proxies (utils/backendProxy.ts) so the app fully works
  // with no external routing wiring. Static mappings only - the backend
  // hosts are resolved from env at runtime inside the API routes, which is
  // what keeps this compatible with `output: standalone` (rewrites are baked
  // at build time, env reads in API routes are not).
  rewrites: async () => [
    { source: '/otlp-http/:path*', destination: '/api/otlp-http/:path*' },
    { source: '/flagservice/:path*', destination: '/api/flagservice/:path*' },
    { source: '/images/:path*', destination: '/api/images/:path*' },
  ],
  compiler: {
    styledComponents: true,
  },
  // Turbopack configuration (Next.js 16 default bundler)
  // Turbopack automatically handles Node.js polyfills for client bundles
  turbopack: {
    // Set root to current directory to avoid confusion with parent lockfile
    root: __dirname,
  },
  // Keep webpack config for backwards compatibility if --webpack flag is used
  webpack: (config, { isServer }) => {
    if (!isServer) {
      config.resolve.fallback.http2 = false;
      config.resolve.fallback.tls = false;
      config.resolve.fallback.net = false;
      config.resolve.fallback.dns = false;
      config.resolve.fallback.fs = false;
    }

    return config;
  },
  env: {
    AD_ADDR,
    CART_ADDR,
    CHECKOUT_ADDR,
    CURRENCY_ADDR,
    PRODUCT_CATALOG_ADDR,
    RECOMMENDATION_ADDR,
    SHIPPING_ADDR,
    LOG_LEVEL,
    OTEL_EXPORTER_OTLP_TRACES_ENDPOINT,
    NEXT_PUBLIC_PLATFORM: ENV_PLATFORM,
    NEXT_PUBLIC_OTEL_SERVICE_NAME: OTEL_SERVICE_NAME,
    NEXT_PUBLIC_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: PUBLIC_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT,
  },
  images: {
    loader: "custom",
    loaderFile: "./utils/imageLoader.js"
  }
};

module.exports = nextConfig;
