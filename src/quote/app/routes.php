<?php
// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0



use OpenTelemetry\API\Globals;
use OpenTelemetry\API\Trace\Span;
use OpenTelemetry\API\Trace\SpanKind;
use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Psr\Log\LoggerInterface;
use Slim\App;

function calculateQuote($jsonObject, LoggerInterface $logger): float
{
    $childSpan = Globals::tracerProvider()->getTracer('manual-instrumentation')
        ->spanBuilder('calculate-quote')
        ->setSpanKind(SpanKind::KIND_INTERNAL)
        ->startSpan();
    $childSpan->addEvent('Calculating quote');

    try {
        if (!array_key_exists('numberOfItems', $jsonObject)) {
            throw new \InvalidArgumentException('numberOfItems not provided');
        }
        // array_key_exists only checks presence; intval() silently coerces
        // any non-numeric value (a string, null, bool, array) to 0 or 1
        // with no error, which previously produced a bogus "successful"
        // quote instead of rejecting the request.
        if (!is_numeric($jsonObject['numberOfItems'])) {
            throw new \InvalidArgumentException('numberOfItems must be numeric');
        }
        $numberOfItems = intval($jsonObject['numberOfItems']);
        $costPerItem = 8.99;
        $quote = round($costPerItem * $numberOfItems, 2);

        $childSpan->setAttribute('demo.shipping.quote.items_count', $numberOfItems);
        $childSpan->setAttribute('demo.shipping.quote.cost.total', $quote);

        $childSpan->addEvent('Quote calculated, returning its value');

        //manual metrics
        static $counter;
        $counter ??= Globals::meterProvider()
            ->getMeter('quotes')
            ->createCounter('quotes', 'quotes', 'number of quotes calculated');
        $counter->add(1, ['number_of_items' => $numberOfItems]);

        return $quote;
    } catch (\InvalidArgumentException $exception) {
        // Malformed request input is a normal client 400, not a system
        // fault - warning, not error, matches severity conventions where
        // "error" drives alerting/paging.
        $childSpan->recordException($exception);
        $logger->warning('Failed to calculate quote', ['exception' => $exception]);
        throw $exception;
    } finally {
        $childSpan->end();
    }
}

return function (App $app) {
    $app->get('/health', function (Request $request, Response $response) {
        return $response->withStatus(200);
    });

    $app->post('/getquote', function (Request $request, Response $response, LoggerInterface $logger) {
        $span = Span::getCurrent();
        $span->addEvent('Received get quote request, processing it');

        $jsonObject = $request->getParsedBody();

        try {
            $data = calculateQuote($jsonObject, $logger);
        } catch (\InvalidArgumentException $exception) {
            $payload = json_encode(['error' => $exception->getMessage()]);
            $response->getBody()->write($payload);

            return $response
                ->withHeader('Content-Type', 'application/json')
                ->withStatus(400);
        }

        $payload = json_encode($data);
        $response->getBody()->write($payload);

        $span->addEvent('Quote processed, response sent back', [
            'demo.shipping.quote.cost.total' => $data
        ]);
        //exported as an opentelemetry log (see dependencies.php)
        $logger->info('Calculated quote', [
            'total' => $data,
        ]);

        return $response
            ->withHeader('Content-Type', 'application/json');
    });
};
