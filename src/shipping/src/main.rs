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

    loop {
        attempt += 1;
        match FlagdProvider::new(FlagdOptions {
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

    let mut ip = "0.0.0.0".to_string();

    if let Ok(ipv6_enabled) = env::var("IPV6_ENABLED") {
        if ipv6_enabled == "true" {
            ip = "[::]".to_string();
            info!("Overwriting Localhost IP:  {ip}");
        }
    }

    let addr = format!("{}:{}", ip, port);
    info!(
        name: "shipping.server.started",
        addr = addr.as_str(),
        message = "Shipping service is running"
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
            .route(
                "/health",
                web::get().to(|| async { HttpResponse::Ok().finish() }),
            )
    })
    .bind(&addr)?
    .run()
    .await?;

    otel_guard.shutdown();
    Ok(())
}
