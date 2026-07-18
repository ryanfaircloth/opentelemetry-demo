// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/log/global"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	otelhooks "github.com/open-feature/go-sdk-contrib/hooks/open-telemetry/pkg"
	flagd "github.com/open-feature/go-sdk-contrib/providers/flagd/pkg"
	"github.com/open-feature/go-sdk/openfeature"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	flags "github.com/open-telemetry/opentelemetry-demo/src/checkout/flags"
	pb "github.com/open-telemetry/opentelemetry-demo/src/checkout/genproto/oteldemo"
	"github.com/open-telemetry/opentelemetry-demo/src/checkout/kafka"
	"github.com/open-telemetry/opentelemetry-demo/src/checkout/money"
)

//go:generate go install google.golang.org/protobuf/cmd/protoc-gen-go
//go:generate go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
//go:generate protoc --go_out=./ --go-grpc_out=./ --proto_path=../../pb ../../pb/demo.proto
//go:generate go install github.com/open-feature/cli/cmd/openfeature@v0.4.0
//go:generate openfeature generate -o flags --package-name flags go

var (
	logger            *slog.Logger
	tracer            trace.Tracer
	resource          *sdkresource.Resource
	initResourcesOnce sync.Once
)

func initResource() *sdkresource.Resource {
	initResourcesOnce.Do(func() {
		extraResources, _ := sdkresource.New(
			context.Background(),
			sdkresource.WithOS(),
			sdkresource.WithProcess(),
			sdkresource.WithContainer(),
			sdkresource.WithHost(),
		)
		resource, _ = sdkresource.Merge(
			sdkresource.Default(),
			extraResources,
		)
	})
	return resource
}

func initTracerProvider() *sdktrace.TracerProvider {
	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		logger.Error(fmt.Sprintf("new otlp trace http exporter failed: %v", err))
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(initResource()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp
}

func initMeterProvider() *sdkmetric.MeterProvider {
	ctx := context.Background()

	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		logger.Error(fmt.Sprintf("new otlp metric http exporter failed: %v", err))
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(initResource()),
	)
	otel.SetMeterProvider(mp)
	return mp
}

func initLoggerProvider() *sdklog.LoggerProvider {
	ctx := context.Background()

	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	global.SetLoggerProvider(loggerProvider)

	return loggerProvider
}

// multiHandler fans out log records to multiple slog.Handlers. It's used so
// logs reach both the OTLP exporter and stdout, keeping WARN+ visible via
// `docker logs`/`kubectl logs` even when the collector is unreachable.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

type checkout struct {
	productCatalogSvcAddr string
	cartSvcAddr           string
	currencySvcAddr       string
	shippingSvcAddr       string
	emailSvcAddr          string
	paymentSvcAddr        string
	kafkaBrokerSvcAddr    string
	pb.UnimplementedCheckoutServiceServer
	KafkaProducerClient     sarama.AsyncProducer
	productCatalogSvcClient pb.ProductCatalogServiceClient
	cartSvcClient           pb.CartServiceClient
	currencySvcClient       pb.CurrencyServiceClient
	paymentSvcClient        pb.PaymentServiceClient
	httpClient              *http.Client
}

func main() {
	var port string
	mustMapEnv(&port, "CHECKOUT_PORT")

	tp := initTracerProvider()
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Error(fmt.Sprintf("Error shutting down tracer provider: %v", err))
		}
	}()

	mp := initMeterProvider()
	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			logger.Error(fmt.Sprintf("Error shutting down meter provider: %v", err))
		}
	}()

	lp := initLoggerProvider()
	defer func() {
		if err := lp.Shutdown(context.Background()); err != nil {
			logger.Error(fmt.Sprintf("Error shutting down logger provider: %v", err))
		}
	}()

	// this *must* be called after the logger provider is initialized
	// otherwise the Sarama producer in kafka/producer.go will not be
	// able to log properly
	//
	// Logs fan out to both the OTLP exporter and stdout, so WARN+ logs
	// (e.g. Kafka/gRPC connection retries) are still visible via
	// `docker logs`/`kubectl logs` if the collector is unreachable.
	otelHandler := otelslog.NewLogger("checkout").Handler()
	stdoutHandler := slog.NewJSONHandler(os.Stdout, nil)
	logger = slog.New(&multiHandler{handlers: []slog.Handler{otelHandler, stdoutHandler}})
	slog.SetDefault(logger)

	// A plain HTTP health endpoint: kubelet's httpGet probe needs no gRPC
	// client tooling and avoids the fragile-precompiled-gencode class of
	// problem gRPC health checking libraries can hit under
	// auto-instrumentation injection (see the recommendation service's
	// RECOMMENDATION_HEALTH_PORT for precedent). Replaces the gRPC health
	// service this service used to register. Started before the blocking
	// dependency waits below so the probe doesn't kill the pod while it's
	// still legitimately waiting on a slow-starting dependency.
	healthPort := os.Getenv("CHECKOUT_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8081"
	}
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		if err := http.ListenAndServe(fmt.Sprintf(":%s", healthPort), mux); err != nil {
			logger.Error(fmt.Sprintf("health check HTTP server failed: %v", err))
		}
	}()

	err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		logger.Error((err.Error()))
	}

	provider, err := flagd.NewProvider()
	if err != nil {
		logger.Error("Error creating flagd provider", slog.Any("error", err))
	}

	err = openfeature.SetProvider(provider)
	if err != nil {
		logger.Error("Failed to set flagd as the provider", slog.Any("error", err))
	}
	defer openfeature.Shutdown()
	openfeature.AddHooks(otelhooks.NewTracesHook())

	tracer = tp.Tracer("checkout")

	svc := new(checkout)
	svc.httpClient = &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	// shipping is called over REST (see quoteShipping/shipOrder below), not gRPC.
	mustMapEnv(&svc.shippingSvcAddr, "SHIPPING_ADDR")

	mustMapEnv(&svc.productCatalogSvcAddr, "PRODUCT_CATALOG_ADDR")
	c := mustCreateClient(svc.productCatalogSvcAddr)
	svc.productCatalogSvcClient = pb.NewProductCatalogServiceClient(c)
	defer c.Close()

	mustMapEnv(&svc.cartSvcAddr, "CART_ADDR")
	c = mustCreateClient(svc.cartSvcAddr)
	svc.cartSvcClient = pb.NewCartServiceClient(c)
	defer c.Close()

	mustMapEnv(&svc.currencySvcAddr, "CURRENCY_ADDR")
	c = mustCreateClient(svc.currencySvcAddr)
	svc.currencySvcClient = pb.NewCurrencyServiceClient(c)
	defer c.Close()

	// email is called over REST (see sendOrderConfirmation below), not gRPC.
	mustMapEnv(&svc.emailSvcAddr, "EMAIL_ADDR")

	mustMapEnv(&svc.paymentSvcAddr, "PAYMENT_ADDR")
	c = mustCreateClient(svc.paymentSvcAddr)
	svc.paymentSvcClient = pb.NewPaymentServiceClient(c)
	defer c.Close()

	svc.kafkaBrokerSvcAddr = os.Getenv("KAFKA_ADDR")

	if svc.kafkaBrokerSvcAddr != "" {
		svc.KafkaProducerClient, err = createKafkaProducerWithRetry(svc.kafkaBrokerSvcAddr)
		if err != nil {
			logger.Error(fmt.Sprintf("Giving up connecting to Kafka: %v", err))
			os.Exit(1)
		}
	}

	logger.Info(fmt.Sprintf("service config: %+v", svc))

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.Error(err.Error())
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
		)),
	)
	pb.RegisterCheckoutServiceServer(srv, svc)

	logger.Info(fmt.Sprintf("starting to listen on tcp: %q", lis.Addr().String()))
	err = srv.Serve(lis)
	logger.Error(err.Error())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Error(err.Error())
		}
	}()

	<-ctx.Done()

	srv.GracefulStop()
	logger.Info("Checkout gRPC server stopped")
}

func mustMapEnv(target *string, envKey string) {
	v := os.Getenv(envKey)
	if v == "" {
		panic(fmt.Sprintf("environment variable %q not set", envKey))
	}
	*target = v
}

// kafkaConnectMaxWait bounds how long createKafkaProducerWithRetry retries a
// failing Kafka connection before giving up. It's intentionally generous -
// the point of retrying in-process is to make an external "wait for kafka"
// init container unnecessary.
const kafkaConnectMaxWait = 2 * time.Minute

// createKafkaProducerWithRetry retries kafka.CreateKafkaProducer with
// backoff. Sarama connects lazily on first Produce(), so a broker that is
// merely slow to come up (rather than misconfigured) wouldn't otherwise
// surface here - it would instead panic later on a nil KafkaProducerClient
// the first time an order is placed.
func createKafkaProducerWithRetry(brokerAddr string) (sarama.AsyncProducer, error) {
	deadline := time.Now().Add(kafkaConnectMaxWait)
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		producer, err := kafka.CreateKafkaProducer([]string{brokerAddr}, logger)
		if err == nil {
			return producer, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to create Kafka producer after %d attempts: %w", attempt, err)
		}
		logger.Warn("Kafka not ready yet, retrying",
			slog.Int("attempt", attempt),
			slog.Duration("backoff", backoff),
			slog.Any("error", err))
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (cs *checkout) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("user.id", req.UserId),
		attribute.String("demo.user_context.selected_currency", req.UserCurrency),
	)

	if baggage.FromContext(ctx).Member("synthetic_request").Value() == "true" {
		span.SetAttributes(attribute.String("user_agent.synthetic.type", "test"))
	}

	logger.LogAttrs(
		ctx,
		slog.LevelInfo, "[PlaceOrder]",
		slog.String("user_id", req.UserId),
		slog.String("user_currency", req.UserCurrency),
	)

	var err error
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	orderID, err := uuid.NewUUID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate order uuid")
	}

	prep, err := cs.prepareOrderItemsAndShippingQuoteFromCart(ctx, req.UserId, req.UserCurrency, req.Address)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	span.AddEvent("prepared")

	total := &pb.Money{
		CurrencyCode: req.UserCurrency,
		Units:        0,
		Nanos:        0,
	}
	total = money.Must(money.Sum(total, prep.shippingCostLocalized))
	for _, it := range prep.orderItems {
		multPrice := money.MultiplySlow(it.Cost, uint32(it.GetItem().GetQuantity()))
		total = money.Must(money.Sum(total, multPrice))
	}

	txID, err := cs.chargeCard(ctx, total, req.CreditCard)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to charge card: %+v", err)
	}

	span.AddEvent("charged",
		trace.WithAttributes(attribute.String("demo.payment.transaction.id", txID)))
	logger.LogAttrs(
		ctx,
		slog.LevelInfo, "payment went through",
		slog.String("transaction_id", txID),
	)

	shippingTrackingID, err := cs.shipOrder(ctx, req.Address, prep.cartItems)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "shipping error: %+v", err)
	}
	shippingTrackingAttribute := attribute.String("demo.shipping.tracking.id", shippingTrackingID)
	span.AddEvent("shipped", trace.WithAttributes(shippingTrackingAttribute))

	_ = cs.emptyUserCart(ctx, req.UserId)

	orderResult := &pb.OrderResult{
		OrderId:            orderID.String(),
		ShippingTrackingId: shippingTrackingID,
		ShippingCost:       prep.shippingCostLocalized,
		ShippingAddress:    req.Address,
		Items:              prep.orderItems,
	}

	shippingCostFloat, _ := strconv.ParseFloat(fmt.Sprintf("%d.%02d", prep.shippingCostLocalized.GetUnits(), prep.shippingCostLocalized.GetNanos()/1000000000), 64)
	totalPriceFloat, _ := strconv.ParseFloat(fmt.Sprintf("%d.%02d", total.GetUnits(), total.GetNanos()/1000000000), 64)

	span.SetAttributes(
		attribute.String("demo.order.id", orderID.String()),
		attribute.Float64("demo.shipping.amount", shippingCostFloat),
		attribute.Float64("demo.order.amount", totalPriceFloat),
		attribute.Int("demo.order.items.count", len(prep.orderItems)),
		shippingTrackingAttribute,
	)
	logger.LogAttrs(
		ctx,
		slog.LevelInfo, "order placed",
		slog.String("demo.order.id", orderID.String()),
		slog.Float64("demo.shipping.amount", shippingCostFloat),
		slog.Float64("demo.order.amount", totalPriceFloat),
		slog.Int("demo.order.items.count", len(prep.orderItems)),
		slog.String("demo.shipping.tracking.id", shippingTrackingID),
	)

	if err := cs.sendOrderConfirmation(ctx, req.Email, orderResult); err != nil {
		logger.Warn(fmt.Sprintf("failed to send order confirmation: %+v", err))
	} else {
		logger.Info("order confirmation email sent")
	}

	// send to kafka only if kafka broker address is set
	if cs.kafkaBrokerSvcAddr != "" {
		logger.Info("sending to postProcessor")
		cs.sendToPostProcessor(ctx, orderResult)
	}

	resp := &pb.PlaceOrderResponse{Order: orderResult}
	return resp, nil
}

type orderPrep struct {
	orderItems            []*pb.OrderItem
	cartItems             []*pb.CartItem
	shippingCostLocalized *pb.Money
}

func (cs *checkout) prepareOrderItemsAndShippingQuoteFromCart(ctx context.Context, userID, userCurrency string, address *pb.Address) (orderPrep, error) {
	ctx, span := tracer.Start(ctx, "prepareOrderItemsAndShippingQuoteFromCart")
	defer span.End()

	var out orderPrep
	cartItems, err := cs.getUserCart(ctx, userID)
	if err != nil {
		return out, fmt.Errorf("cart failure: %+v", err)
	}
	orderItems, err := cs.prepOrderItems(ctx, cartItems, userCurrency)
	if err != nil {
		return out, fmt.Errorf("failed to prepare order: %+v", err)
	}
	shippingUSD, err := cs.quoteShipping(ctx, address, cartItems)
	if err != nil {
		return out, fmt.Errorf("shipping quote failure: %+v", err)
	}
	shippingPrice, err := cs.convertCurrency(ctx, shippingUSD, userCurrency)
	if err != nil {
		return out, fmt.Errorf("failed to convert shipping cost to currency: %+v", err)
	}

	out.shippingCostLocalized = shippingPrice
	out.cartItems = cartItems
	out.orderItems = orderItems

	var totalCart int32
	for _, ci := range cartItems {
		totalCart += ci.Quantity
	}
	shippingCostFloat, _ := strconv.ParseFloat(fmt.Sprintf("%d.%02d", shippingPrice.GetUnits(), shippingPrice.GetNanos()/1000000000), 64)

	span.SetAttributes(
		attribute.Float64("demo.shipping.amount", shippingCostFloat),
		attribute.Int("demo.cart.items.count", int(totalCart)),
		attribute.Int("demo.order.items.count", len(orderItems)),
	)
	return out, nil
}

// grpcConnectMaxWait bounds how long mustCreateClient retries a failing
// downstream gRPC connection before giving up. Mirrors kafkaConnectMaxWait -
// checkout's downstream services are all required for it to function, so a
// connection that never comes up should be a fatal startup error rather than
// a broken client that fails on first use.
const grpcConnectMaxWait = 2 * time.Minute

func mustCreateClient(svcAddr string) *grpc.ClientConn {
	c, err := grpc.NewClient(svcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		logger.Error(fmt.Sprintf("could not create client for %s service, err: %+v", svcAddr, err))
		os.Exit(1)
	}

	if err := waitForConnectionReady(c, svcAddr); err != nil {
		logger.Error(fmt.Sprintf("giving up connecting to %s service: %v", svcAddr, err))
		os.Exit(1)
	}

	return c
}

// waitForConnectionReady blocks until conn leaves its initial idle state and
// reaches Ready (or Idle, if it settles there between attempts), retrying
// with exponential backoff. grpc.NewClient's connection is lazy/non-blocking,
// so without this a connectivity problem wouldn't surface until the first
// RPC call.
func waitForConnectionReady(conn *grpc.ClientConn, svcAddr string) error {
	deadline := time.Now().Add(grpcConnectMaxWait)
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}

		conn.Connect()
		waitCtx, cancel := context.WithTimeout(context.Background(), backoff)
		conn.WaitForStateChange(waitCtx, state)
		cancel()

		newState := conn.GetState()
		if newState == connectivity.Ready || newState == connectivity.Idle {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("connection to %s not ready after %d attempts, last state: %s", svcAddr, attempt, newState)
		}
		logger.Warn("service not ready yet, retrying",
			slog.String("addr", svcAddr),
			slog.Int("attempt", attempt),
			slog.String("state", newState.String()),
			slog.Duration("backoff", backoff))
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (cs *checkout) quoteShipping(ctx context.Context, address *pb.Address, items []*pb.CartItem) (*pb.Money, error) {
	quotePayload, err := json.Marshal(map[string]interface{}{
		"address": address,
		"items":   items,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ship order request: %+v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cs.shippingSvcAddr+"/get-quote", bytes.NewBuffer(quotePayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %+v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cs.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed POST to shipping service: %+v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed POST to email service: expected 200, got %d", resp.StatusCode)
	}

	shippingQuoteBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read shipping quote response: %+v", err)
	}

	var quoteResp struct {
		CostUsd *pb.Money `json:"cost_usd"`
	}
	if err := json.Unmarshal(shippingQuoteBytes, &quoteResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shipping quote: %+v", err)
	}
	if quoteResp.CostUsd == nil {
		return nil, fmt.Errorf("shipping quote missing cost_usd field")
	}

	return quoteResp.CostUsd, nil
}

func (cs *checkout) getUserCart(ctx context.Context, userID string) ([]*pb.CartItem, error) {
	cart, err := cs.cartSvcClient.GetCart(ctx, &pb.GetCartRequest{UserId: userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get user cart during checkout: %+v", err)
	}
	return cart.GetItems(), nil
}

func (cs *checkout) emptyUserCart(ctx context.Context, userID string) error {
	if _, err := cs.cartSvcClient.EmptyCart(ctx, &pb.EmptyCartRequest{UserId: userID}); err != nil {
		return fmt.Errorf("failed to empty user cart during checkout: %+v", err)
	}
	return nil
}

func (cs *checkout) prepOrderItems(ctx context.Context, items []*pb.CartItem, userCurrency string) ([]*pb.OrderItem, error) {
	out := make([]*pb.OrderItem, len(items))

	for i, item := range items {
		product, err := cs.productCatalogSvcClient.GetProduct(ctx, &pb.GetProductRequest{Id: item.GetProductId()})
		if err != nil {
			return nil, fmt.Errorf("failed to get product #%q", item.GetProductId())
		}
		price, err := cs.convertCurrency(ctx, product.GetPriceUsd(), userCurrency)
		if err != nil {
			return nil, fmt.Errorf("failed to convert price of %q to %s", item.GetProductId(), userCurrency)
		}
		out[i] = &pb.OrderItem{
			Item: item,
			Cost: price,
		}
	}
	return out, nil
}

func (cs *checkout) convertCurrency(ctx context.Context, from *pb.Money, toCurrency string) (*pb.Money, error) {
	result, err := cs.currencySvcClient.Convert(ctx, &pb.CurrencyConversionRequest{
		From:   from,
		ToCode: toCurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert currency: %+v", err)
	}
	return result, err
}

// paymentChargeTimeout bounds how long checkout waits for the payment
// service's Charge RPC. The "paymentUnreachable" flag is simulated
// server-side in payment (see src/payment/charge.js) by delaying its
// response past this deadline, so this timeout is what actually turns
// that delay into a failed charge instead of an indefinite hang.
const paymentChargeTimeout = 5 * time.Second

func (cs *checkout) chargeCard(ctx context.Context, amount *pb.Money, paymentInfo *pb.CreditCardInfo) (string, error) {
	chargeCtx, cancel := context.WithTimeout(ctx, paymentChargeTimeout)
	defer cancel()

	paymentResp, err := cs.paymentSvcClient.Charge(chargeCtx, &pb.ChargeRequest{
		Amount:     amount,
		CreditCard: paymentInfo,
	})
	if err != nil {
		return "", fmt.Errorf("could not charge the card: %+v", err)
	}
	return paymentResp.GetTransactionId(), nil
}

func (cs *checkout) sendOrderConfirmation(ctx context.Context, email string, order *pb.OrderResult) error {
	emailPayload, err := json.Marshal(map[string]interface{}{
		"email": email,
		"order": order,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal order to JSON: %+v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cs.emailSvcAddr+"/send_order_confirmation", bytes.NewBuffer(emailPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %+v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cs.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed POST to email service: %+v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed POST to email service: expected 200, got %d", resp.StatusCode)
	}

	return err
}

func (cs *checkout) shipOrder(ctx context.Context, address *pb.Address, items []*pb.CartItem) (string, error) {
	shipPayload, err := json.Marshal(map[string]interface{}{
		"address": address,
		"items":   items,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal ship order request: %+v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cs.shippingSvcAddr+"/ship-order", bytes.NewBuffer(shipPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %+v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cs.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed POST to shipping service: %+v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed POST to email service: expected 200, got %d", resp.StatusCode)
	}

	trackingRespBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read ship order response: %+v", err)
	}

	var shipResp struct {
		TrackingID string `json:"tracking_id"`
	}
	if err := json.Unmarshal(trackingRespBytes, &shipResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal ship order response: %+v", err)
	}
	if shipResp.TrackingID == "" {
		return "", fmt.Errorf("ship order response missing tracking_id field")
	}

	return shipResp.TrackingID, nil
}

func (cs *checkout) sendToPostProcessor(ctx context.Context, result *pb.OrderResult) {
	message, err := proto.Marshal(result)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to marshal message to protobuf: %+v", err))
		return
	}

	msg := sarama.ProducerMessage{
		Topic: kafka.Topic,
		Value: sarama.ByteEncoder(message),
	}

	// Inject tracing info into message
	span := createProducerSpan(ctx, &msg)
	defer span.End()

	// Send message and handle response
	startTime := time.Now()
	select {
	case cs.KafkaProducerClient.Input() <- &msg:
		select {
		case successMsg := <-cs.KafkaProducerClient.Successes():
			span.SetAttributes(
				attribute.Bool("messaging.kafka.producer.success", true),
				attribute.Int("messaging.kafka.producer.duration_ms", int(time.Since(startTime).Milliseconds())),
				attribute.KeyValue(semconv.MessagingKafkaMessageOffset(int(successMsg.Offset))),
			)
			logger.Info(fmt.Sprintf("Successful to write message. offset: %v, duration: %v", successMsg.Offset, time.Since(startTime)))
		case errMsg := <-cs.KafkaProducerClient.Errors():
			span.SetAttributes(
				attribute.Bool("messaging.kafka.producer.success", false),
				attribute.Int("messaging.kafka.producer.duration_ms", int(time.Since(startTime).Milliseconds())),
			)
			span.SetStatus(otelcodes.Error, errMsg.Err.Error())
			logger.Error(fmt.Sprintf("Failed to write message: %v", errMsg.Err))
		case <-ctx.Done():
			span.SetAttributes(
				attribute.Bool("messaging.kafka.producer.success", false),
				attribute.Int("messaging.kafka.producer.duration_ms", int(time.Since(startTime).Milliseconds())),
			)
			span.SetStatus(otelcodes.Error, "Context cancelled: "+ctx.Err().Error())
			logger.Warn(fmt.Sprintf("Context canceled before success message received: %v", ctx.Err()))
		}
	case <-ctx.Done():
		span.SetAttributes(
			attribute.Bool("messaging.kafka.producer.success", false),
			attribute.Int("messaging.kafka.producer.duration_ms", int(time.Since(startTime).Milliseconds())),
		)
		span.SetStatus(otelcodes.Error, "Failed to send: "+ctx.Err().Error())
		logger.Error(fmt.Sprintf("Failed to send message to Kafka within context deadline: %v", ctx.Err()))
		return
	}

	ffValue := flags.KafkaQueueProblems.Value(ctx, openfeature.EvaluationContext{})
	if ffValue > 0 {
		logger.Info("Warning: FeatureFlag 'kafkaQueueProblems' is activated, overloading queue now.")
		for range ffValue {
			go func(msg sarama.ProducerMessage) {
				cs.KafkaProducerClient.Input() <- &msg
				<-cs.KafkaProducerClient.Successes()
			}(msg)
		}
		logger.Info(fmt.Sprintf("Done with #%d messages for overload simulation.", ffValue))
	}
}

func createProducerSpan(ctx context.Context, msg *sarama.ProducerMessage) trace.Span {
	spanContext, span := tracer.Start(
		ctx,
		fmt.Sprintf("%s publish", msg.Topic),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.PeerService("kafka"),
			semconv.NetworkTransportTCP,
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(msg.Topic),
			semconv.MessagingOperationPublish,
			semconv.MessagingKafkaDestinationPartition(int(msg.Partition)),
		),
	)

	carrier := propagation.MapCarrier{}
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(spanContext, carrier)

	for key, value := range carrier {
		msg.Headers = append(msg.Headers, sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
	}

	return span
}
