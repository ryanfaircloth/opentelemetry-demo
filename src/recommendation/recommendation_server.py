#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0


# Python
import os
import random
import socket
import sys
import threading
import time
from concurrent import futures
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Pip
import grpc
from opentelemetry import trace, metrics
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import (
    OTLPLogExporter,
)
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource

from openfeature import api
from openfeature.contrib.provider.flagd import FlagdProvider

from openfeature.contrib.hook.opentelemetry import TracingHook

# Local
import logging
import demo_pb2
import demo_pb2_grpc

from metrics import (
    init_metrics
)

cached_ids = []
first_run = True

class RecommendationService(demo_pb2_grpc.RecommendationServiceServicer):
    def ListRecommendations(self, request, context):
        prod_list = get_product_list(request.product_ids)
        span = trace.get_current_span()
        span.set_attribute("demo.product.recommended.count", len(prod_list))
        logger.info(f"Receive ListRecommendations for product ids:{prod_list}")

        # build and return response
        response = demo_pb2.ListRecommendationsResponse()
        response.product_ids.extend(prod_list)

        # Collect metrics for this service
        rec_svc_metrics["demo.recommendation.requests"].add(len(prod_list), {'recommendation.type': 'catalog'})

        return response


class HealthCheckHandler(BaseHTTPRequestHandler):
    """Plain HTTP health endpoint, deliberately independent of gRPC/protobuf:
    a gRPC health check would pull in grpc_health's bundled protobuf gencode,
    whose version must stay compatible with whatever protobuf runtime an
    externally injected auto-instrumentation agent provides - a coincidental
    alignment that has already broken this service once (see CHANGELOG)."""

    def do_GET(self):
        if self.path == '/healthz':
            self.send_response(200)
            self.end_headers()
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        pass


class DualStackHTTPServer(ThreadingHTTPServer):
    """http.server binds AF_INET (IPv4-only) by default; on IPv6-only pod
    networks that leaves kubelet's probe unable to reach it at all."""

    address_family = socket.AF_INET6


def get_product_list(request_product_ids):
    global first_run
    global cached_ids
    with tracer.start_as_current_span("get_product_list") as span:
        max_responses = 5

        # Formulate the list of characters to list of strings
        request_product_ids_str = ''.join(request_product_ids)
        request_product_ids = request_product_ids_str.split(',')

        # Feature flag scenario - Cache Leak
        if check_feature_flag("recommendationCacheFailure"):
            span.set_attribute("demo.feature_flag.recommendation_cache", True)
            if random.random() < 0.5 or first_run:
                first_run = False
                span.set_attribute("demo.recommendation.cache_hit", False)
                logger.info("get_product_list: cache miss")
                cat_response = product_catalog_stub.ListProducts(demo_pb2.Empty())
                response_ids = [x.id for x in cat_response.products]
                cached_ids = cached_ids + response_ids
                cached_ids = cached_ids + cached_ids[:len(cached_ids) // 4]
                product_ids = cached_ids
            else:
                span.set_attribute("demo.recommendation.cache_hit", True)
                logger.info("get_product_list: cache hit")
                product_ids = cached_ids
        else:
            span.set_attribute("demo.feature_flag.recommendation_cache", False)
            cat_response = product_catalog_stub.ListProducts(demo_pb2.Empty())
            product_ids = [x.id for x in cat_response.products]

        span.set_attribute("demo.product.count", len(product_ids))

        # Create a filtered list of products excluding the products received as input
        filtered_products = list(set(product_ids) - set(request_product_ids))
        num_products = len(filtered_products)
        span.set_attribute("demo.product.filtered.count", num_products)
        num_return = min(max_responses, num_products)

        # Sample list of indices to return
        indices = random.sample(range(num_products), num_return)
        # Fetch product ids from indices
        prod_list = [filtered_products[i] for i in indices]

        span.set_attribute("demo.product.filtered.list", prod_list)

        return prod_list


def must_map_env(key: str):
    value = os.environ.get(key)
    if value is None:
        raise Exception(f'{key} environment variable must be set')
    return value


def check_feature_flag(flag_name: str):
    # Initialize OpenFeature
    client = api.get_client()
    return client.get_boolean_value("recommendationCacheFailure", False)


if __name__ == "__main__":
    service_name = must_map_env('OTEL_SERVICE_NAME')
    api.set_provider(FlagdProvider(host=os.environ.get('FLAGD_HOST', 'flagd'), port=os.environ.get('FLAGD_PORT', 8013)))
    api.add_hooks([TracingHook()])

    # Initialize Traces and Metrics
    tracer = trace.get_tracer_provider().get_tracer(service_name)
    meter = metrics.get_meter_provider().get_meter(service_name)
    rec_svc_metrics = init_metrics(meter)

    # Initialize Logs
    logger_provider = LoggerProvider(
        resource = Resource.create({}),
    )
    set_logger_provider(logger_provider)
    log_exporter = OTLPLogExporter(insecure=True)
    logger_provider.add_log_record_processor(BatchLogRecordProcessor(log_exporter))
    handler = LoggingHandler(level=logging.NOTSET, logger_provider=logger_provider)

    # Attach OTLP handler to logger, plus a console handler so WARN+ is
    # always visible even if the collector is unreachable
    logger = logging.getLogger('main')
    logger.addHandler(handler)
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(logging.WARNING)
    logger.addHandler(console_handler)
    logger.setLevel(logging.INFO)

    # Started before the blocking product-catalog wait below so the probe
    # doesn't kill the pod while it's still legitimately waiting on a
    # slow-starting dependency.
    health_port = os.environ.get('RECOMMENDATION_HEALTH_PORT', '8081')
    health_server = DualStackHTTPServer(('::', int(health_port)), HealthCheckHandler)
    threading.Thread(target=health_server.serve_forever, daemon=True).start()
    logger.info(f'Health check endpoint listening on port {health_port}')

    catalog_addr = must_map_env('PRODUCT_CATALOG_ADDR')
    # grpc.enable_retries=0: works around a longstanding grpcio C-core bug
    # (call combiner ref-count assertion, e.g. "Check failed: prev_size >= 1u"
    # in call_combiner.cc) tied to gRPC's internal retry/cancellation
    # machinery - see grpc/grpc#26537, grpc/grpc#38251. We don't set a retry
    # policy ourselves; this disables gRPC's own implicit core-level retries.
    pc_channel = grpc.insecure_channel(catalog_addr, options=(('grpc.enable_retries', 0),))
    product_catalog_stub = demo_pb2_grpc.ProductCatalogServiceStub(pc_channel)

    # product-catalog is a hard dependency; wait for the channel to become
    # ready with backoff before serving, rather than failing on first use
    wait_start = time.time()
    total_budget_seconds = 120
    backoff_seconds = 1
    while True:
        try:
            grpc.channel_ready_future(pc_channel).result(timeout=backoff_seconds)
            break
        except grpc.FutureTimeoutError:
            elapsed = time.time() - wait_start
            if elapsed >= total_budget_seconds:
                logger.error(
                    f'product-catalog at {catalog_addr} did not become ready within '
                    f'{total_budget_seconds}s; exiting'
                )
                sys.exit(1)
            logger.warning(
                f'product-catalog at {catalog_addr} not ready yet, retrying '
                f'(elapsed {elapsed:.0f}s/{total_budget_seconds}s)'
            )
            backoff_seconds = min(backoff_seconds * 2, 30)

    # Create gRPC server
    # grpc.enable_retries=0: same call-combiner workaround as the
    # product-catalog channel above (grpc/grpc#26537, grpc/grpc#38251) -
    # the assertion lives in shared C-core machinery, so inbound RPCs
    # (e.g. a client cancelling mid-call) can trigger it too.
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=10),
        options=(('grpc.enable_retries', 0),),
    )

    # Add class to gRPC server
    service = RecommendationService()
    demo_pb2_grpc.add_RecommendationServiceServicer_to_server(service, server)

    # Start server
    port = must_map_env('RECOMMENDATION_PORT')
    server.add_insecure_port(f'[::]:{port}')
    server.start()
    logger.info(f'Recommendation service started, listening on port {port}')

    server.wait_for_termination()
