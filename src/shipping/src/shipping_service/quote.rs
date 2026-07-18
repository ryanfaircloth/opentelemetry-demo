// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

use core::fmt;
use opentelemetry::global;
use opentelemetry::metrics::Counter;
use opentelemetry_instrumentation_actix_web::ClientExt;
use std::sync::LazyLock;
use std::{collections::HashMap, env};

use anyhow::{Context, Result};
use opentelemetry::{trace::get_active_span, KeyValue};
use std::time::Duration;
use tracing::{info, warn};

use super::shipping_types::Quote;

static ITEMS_SHIPPED_COUNTER: LazyLock<Counter<u64>> = LazyLock::new(|| {
    global::meter("otel_demo.shipping.quote")
        .u64_counter("demo.shipping.items_shipped")
        .build()
});

// Resolved once at startup rather than per request: QUOTE_ADDR is a fixed
// deployment-time env var, not something that can change between requests.
static QUOTE_SERVICE_BASE_ADDR: LazyLock<String> =
    LazyLock::new(|| env::var("QUOTE_ADDR").unwrap_or_else(|_| "http://quote:8090".to_string()));

pub async fn create_quote_from_count(count: u32) -> Result<Quote, tonic::Status> {
    let f = match request_quote(count).await {
        Ok(float) => float,
        Err(err) => {
            let msg = format!("{}", err);
            warn!("Failed to get quote from quote service: {}", msg);
            return Err(tonic::Status::unknown(msg));
        }
    };

    ITEMS_SHIPPED_COUNTER.add(count as u64, &[]);

    Ok(get_active_span(|span| {
        let q = create_quote_from_float(f);
        if span.is_recording() {
            let cost = format!("{}", q);
            span.add_event(
                "shipping.quote.received".to_string(),
                vec![KeyValue::new("demo.shipping.cost.total", cost.clone())],
            );
            span.set_attribute(KeyValue::new("demo.shipping.cost.total", cost));
        }
        q
    }))
}

async fn request_quote(count: u32) -> Result<f64, anyhow::Error> {
    let client = awc::Client::new();
    let quote_service_addr: String = format!("{}/getquote", *QUOTE_SERVICE_BASE_ADDR);

    info!(
        name: "shipping.quote.requested",
        quote_service_addr = quote_service_addr.as_str(),
        "Requesting quote"
    );

    let mut reqbody = HashMap::new();
    reqbody.insert("numberOfItems", count);

    // Only the initial connection/send is retried here: transient network
    // errors (e.g. connection refused/timeout) are worth a couple of quick
    // retries, but a response that was successfully sent and read is an
    // application-level result that should not be retried.
    const MAX_ATTEMPTS: u32 = 3;
    let mut delay = Duration::from_millis(300);
    let mut attempt: u32 = 0;

    let mut response = loop {
        attempt += 1;
        match client
            .post(quote_service_addr.clone())
            .trace_request()
            .send_json(&reqbody)
            .await
        {
            Ok(response) => break response,
            Err(err) if attempt < MAX_ATTEMPTS => {
                warn!(
                    "Attempt {} to call quote service failed: {}. Retrying in {:?}",
                    attempt, err, delay
                );
                actix_web::rt::time::sleep(delay).await;
                delay *= 2;
            }
            Err(err) => {
                return Err(anyhow::anyhow!("Failed to call quote service: {err}"));
            }
        }
    };

    let bytes = response
        .body()
        .await
        .context("Failed to read response body from quote service")?;

    let resp = std::str::from_utf8(&bytes)
        .context("Failed to parse quote service response as UTF-8")?
        .to_owned();

    let f = resp
        .parse::<f64>()
        .context("Failed to parse quote value as f64")?;

    Ok(f)
}

pub fn create_quote_from_float(value: f64) -> Quote {
    Quote {
        dollars: value.floor() as u64,
        cents: ((value * 100_f64) as u32) % 100,
    }
}

impl fmt::Display for Quote {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}.{}", self.dollars, self.cents)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_create_quote_from_float() {
        let quote = create_quote_from_float(10.99);
        assert_eq!(quote.dollars, 10);
        assert_eq!(quote.cents, 99);

        let quote = create_quote_from_float(0.01);
        assert_eq!(quote.dollars, 0);
        assert_eq!(quote.cents, 1);

        let quote = create_quote_from_float(100.00);
        assert_eq!(quote.dollars, 100);
        assert_eq!(quote.cents, 0);
    }

    #[test]
    fn test_quote_display() {
        let quote = Quote {
            dollars: 10,
            cents: 99,
        };
        assert_eq!(format!("{}", quote), "10.99");

        let quote = Quote {
            dollars: 0,
            cents: 1,
        };
        assert_eq!(format!("{}", quote), "0.1");
    }
}
