// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// installProductionGlobals mirrors what extotel.InitOpenTelemetry does at
// startup. The global propagator defaults to a no-op, so a test that leaves it
// alone proves nothing about propagation: headers would be absent regardless.
func installProductionGlobals(t *testing.T) *sdktrace.TracerProvider {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder()))
	prevProvider := otel.GetTracerProvider()
	prevPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevProvider)
		otel.SetTextMapPropagator(prevPropagator)
	})
	return tp
}

// A probe must produce a client span on the action's trace.
//
// This is a regression test for a silent failure, not a restatement of the
// implementation: otelhttp derives its tracer from the parent span in the
// request context when it is not given a provider, and the parent here is the
// non-recording span that carries the action's span context into the checker.
// A non-recording span reports a noop TracerProvider, so without an explicit
// WithTracerProvider the transport emits nothing at all — the checks still
// pass, the traces just never appear.
func TestProbeRequestsEmitClientSpansOnTheActionTrace(t *testing.T) {
	installProductionGlobals(t)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)

	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	state := &HTTPCheckState{
		URL:               *target,
		Method:            http.MethodGet,
		ReadTimeout:       5 * time.Second,
		ConnectionTimeout: 5 * time.Second,
		Headers:           map[string]string{"traceparent": "operator-supplied"},
	}

	// Stand in for the action's server span, then carry only its span context
	// forward the way newHttpChecker does.
	actionCtx, actionSpan := tp.Tracer("test").Start(context.Background(), "action")
	wantTrace := trace.SpanContextFromContext(actionCtx).TraceID()
	probeCtx := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(actionCtx))

	client := createHttpClient(state)
	req, err := createRequest(probeCtx, state)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	actionSpan.End()

	require.NoError(t, tp.ForceFlush(context.Background()))

	var clientSpans int
	for _, span := range recorder.Ended() {
		if span.SpanKind() != trace.SpanKindClient {
			continue
		}
		clientSpans++
		assert.Equal(t, wantTrace, span.SpanContext().TraceID(),
			"probe span %q should be on the action's trace", span.Name())
	}
	assert.NotZero(t, clientSpans, "the probe request should have produced a client span")

	// The default propagator injects via HeaderCarrier.Set, which overwrites; with
	// propagation suppressed a header the operator configured survives.
	assert.Equal(t, "operator-supplied", gotHeaders.Get("traceparent"),
		"a user-configured header must not be clobbered by trace context injection")
}

// A check's target is arbitrary user input and often a third party, so probes
// must not carry trace context off the network. The span is still recorded
// locally — suppressing propagation must not suppress observability.
func TestProbeRequestsDoNotPropagateTraceContext(t *testing.T) {
	installProductionGlobals(t)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)

	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	state := &HTTPCheckState{
		URL:               *target,
		Method:            http.MethodGet,
		ReadTimeout:       5 * time.Second,
		ConnectionTimeout: 5 * time.Second,
	}

	actionCtx, actionSpan := tp.Tracer("test").Start(context.Background(), "action")
	probeCtx := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(actionCtx))

	client := createHttpClient(state)
	req, err := createRequest(probeCtx, state)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	actionSpan.End()
	require.NoError(t, tp.ForceFlush(context.Background()))

	for _, header := range []string{"Traceparent", "Tracestate", "Baggage"} {
		assert.Empty(t, gotHeaders.Get(header),
			"%s must not be sent to a check target, which is arbitrary user-supplied input", header)
	}

	var clientSpans int
	for _, span := range recorder.Ended() {
		if span.SpanKind() == trace.SpanKindClient {
			clientSpans++
		}
	}
	assert.NotZero(t, clientSpans, "the probe span must still be recorded locally")
}
