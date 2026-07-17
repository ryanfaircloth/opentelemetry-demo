// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

use actix_web::{web, App, HttpResponse, HttpServer};
use open_feature::provider::FeatureProvider;
use open_feature_flagd::{FlagdOptions, FlagdProvider};
use opentelemetry_instrumentation_actix_web::{RequestMetrics, RequestTracing};
use std::env;
use std::sync::Arc;
use std::time::Duration;
use tracing::{error, info, warn};

mod telemetry_conf;
use telemetry_conf::init_otel;
mod shipping_service;
use shipping_service::{get_quote, ship_order};

/// Initializes the flagd provider, retrying with exponential backoff on
/// failure instead of panicking on the first transient connection error.
async fn init_flagd_provider_with_retry() -> FlagdProvider {
    let total_budget = Duration::from_secs(120);
    let mut delay = Duration::from_secs(1);
    let max_delay = Duration::from_secs(30);
    let start = std::time::Instant::now();
    let mut attempt: u32 = 0;

    let host = env::var("FLAGD_HOST").unwrap_or_else(|_| "localhost".to_string());
    let port: u16 = env::var("FLAGD_PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8013);

    loop {
        attempt += 1;
        match FlagdProvider::new(FlagdOptions {
            host: host.clone(),
            port,
            cache_settings: None,
            ..Default::default()
        })
        .await
        {
            Ok(provider) => return provider,
            Err(err) => {
                if start.elapsed() >= total_budget {
                    error!(
                        "Failed to initialize flagd provider after {} attempts over {:?}: {}",
                        attempt,
                        start.elapsed(),
                        err
                    );
                    std::process::exit(1);
                }

                warn!(
                    "Attempt {} to initialize flagd provider failed: {}. Retrying in {:?}",
                    attempt, err, delay
                );

                actix_web::rt::time::sleep(delay).await;
                delay = std::cmp::min(delay * 2, max_delay);
            }
        }
    }
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let otel_guard = match init_otel() {
        Ok(guard) => {
            info!("Successfully configured OTel");
            guard
        }
        Err(err) => {
            panic!("Couldn't start OTel: {0}", err);
        }
    };

    let port: u16 = env::var("SHIPPING_PORT")
        .expect("$SHIPPING_PORT is not set")
        .parse()
        .expect("$SHIPPING_PORT is not a valid port");

    // "[::]" binds dual-stack (IPv4 and IPv6) by default on Linux.
    let addr = format!("[::]:{}", port);
    info!(
        name: "shipping.server.started",
        addr = addr.as_str(),
        message = "Shipping service is running"
    );

    // A plain HTTP health endpoint on its own port, started immediately and
    // independent of the flagd retry below: the main App below doesn't start
    // listening on SHIPPING_PORT until flagd is ready, so a probe against
    // that port would kill the pod while it's still legitimately waiting on
    // a slow-starting flagd.
    let health_port: u16 = env::var("SHIPPING_HEALTH_PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8081);
    let health_addr = format!("[::]:{}", health_port);
    actix_web::rt::spawn(
        HttpServer::new(|| {
            App::new().route(
                "/health",
                web::get().to(|| async { HttpResponse::Ok().finish() }),
            )
        })
        .bind(health_addr)?
        .run(),
    );

    let provider = init_flagd_provider_with_retry().await;

    let flag_provider = web::Data::from(Arc::new(provider) as Arc<dyn FeatureProvider>);

    HttpServer::new(move || {
        App::new()
            .app_data(flag_provider.clone())
            .wrap(RequestTracing::new())
            .wrap(RequestMetrics::default())
            .service(get_quote)
            .service(ship_order)
    })
    .bind(&addr)?
    .run()
    .await?;

    otel_guard.shutdown();
    Ok(())
}
