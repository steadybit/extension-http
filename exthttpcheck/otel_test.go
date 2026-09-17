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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

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
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
