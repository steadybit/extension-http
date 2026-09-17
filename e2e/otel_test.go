// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package e2e

import (
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// otlpCollectorPort is the port the in-test OTLP receiver listens on. The
// extension reaches it from inside minikube via host.minikube.internal, the same
// way the HTTP check tests reach the local TLS servers.
const otlpCollectorPort = 4318

// otlpCollector is a minimal OTLP/HTTP trace receiver standing in for the
// collector an operator would point the extension at.
type otlpCollector struct {
	server *http.Server

	mu    sync.Mutex
	spans []*tracepb.Span
}

func startOtlpCollector(t *testing.T, addr string) *otlpCollector {
	t.Helper()

	c := &otlpCollector{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", c.handleTraces)
	c.server = &http.Server{Addr: addr, Handler: mux}

	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "failed to listen for OTLP traces on %s", addr)

	go func() {
		if err := c.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("OTLP collector stopped unexpectedly")
		}
	}()

	return c
}

func (c *otlpCollector) handleTraces(w http.ResponseWriter, r *http.Request) {
	body, err := readPossiblyGzipped(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			c.spans = append(c.spans, ss.GetSpans()...)
		}
	}
	c.mu.Unlock()

	resp, err := proto.Marshal(&coltracepb.ExportTraceServiceResponse{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(resp)
}

func readPossiblyGzipped(r *http.Request) ([]byte, error) {
	if r.Header.Get("Content-Encoding") != "gzip" {
		return io.ReadAll(r.Body)
	}
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	return io.ReadAll(gz)
}

func (c *otlpCollector) received() []*tracepb.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*tracepb.Span(nil), c.spans...)
}

func (c *otlpCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = nil
}

func (c *otlpCollector) close() {
	if err := c.server.Close(); err != nil {
		log.Error().Err(err).Msg("Failed to close OTLP collector")
	}
}

// otelExtraArgs configures the extension to export traces to the in-test
// collector. The short schedule delay keeps the batch processor flushing while
// the action runs, since the extension is not restarted between test cases.
// firstEnvIndex continues the extraEnv[] list the caller has already started.
func otelExtraArgs(firstEnvIndex int) []string {
	env := []struct{ name, value string }{
		{"OTEL_EXPORTER_OTLP_ENDPOINT", fmt.Sprintf("http://host.minikube.internal:%d", otlpCollectorPort)},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"},
		{"OTEL_SERVICE_NAME", "extension-http"},
		{"OTEL_BSP_SCHEDULE_DELAY", "1000"},
	}

	var args []string
	for i, e := range env {
		idx := strconv.Itoa(firstEnvIndex + i)
		// --set-string: helm would otherwise type OTEL_BSP_SCHEDULE_DELAY as an
		// integer, and a container env value must be a string.
		args = append(args,
			"--set-string", "extraEnv["+idx+"].name="+e.name,
			"--set-string", "extraEnv["+idx+"].value="+e.value,
		)
	}
	return args
}

// The point of the OTEL work is that an operator debugging a timing-out action
// can see, in their tracing backend, both the agent's call into the extension
// and the probes the extension made because of it. This asserts exactly that:
// the action's server spans are exported, the probe requests are exported as
// client spans, and the probes hang off the action's trace rather than floating
// in traces of their own.
func testOtelTracing(collector *otlpCollector, actionID string) func(*testing.T, *e2e.Minikube, *e2e.Extension) {
	return func(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
		collector.reset()

		// Reuse the shared config shape so this test does not drift from the
		// other check tests (it also carries the required headers entry).
		config := httpCheckConfig(testcase{
			url:                "https://host.minikube.internal:8443",
			timeout:            5000,
			insecureSkipVerify: false,
		})
		config["requestsPerSecond"] = 2.0

		action, err := e.RunAction(actionID, nil, config, nil)
		require.NoError(t, err)
		defer func() { _ = action.Cancel() }()
		require.NoError(t, action.Wait())

		var prepareSpan *tracepb.Span
		var probeSpans []*tracepb.Span

		// The batch processor flushes on its schedule delay, so give it a few
		// cycles after the action finished before concluding nothing arrived.
		require.Eventually(t, func() bool {
			prepareSpan, probeSpans = nil, nil
			for _, span := range collector.received() {
				switch {
				case span.GetKind() == tracepb.Span_SPAN_KIND_SERVER && strings.HasSuffix(span.GetName(), "/prepare"):
					prepareSpan = span
				case span.GetKind() == tracepb.Span_SPAN_KIND_CLIENT:
					probeSpans = append(probeSpans, span)
				}
			}
			return prepareSpan != nil && len(probeSpans) > 0
		}, 30*time.Second, 500*time.Millisecond, "expected a server span for /prepare and at least one client span for the probes; received: %v", spanSummary(collector.received()))

		assert.True(t, strings.HasPrefix(prepareSpan.GetName(), "POST /"),
			"server span should be named by method and route, got %q", prepareSpan.GetName())

		// The probes are issued by goroutines that outlive the Prepare request,
		// so this is what proves the span context is carried across into the
		// checker rather than lost when Prepare returns.
		wantTrace := hex.EncodeToString(prepareSpan.GetTraceId())
		var probesOnActionTrace int
		for _, span := range probeSpans {
			if hex.EncodeToString(span.GetTraceId()) == wantTrace {
				probesOnActionTrace++
			}
		}
		assert.NotZero(t, probesOnActionTrace,
			"probe client spans should share the action's trace %s, got %d client spans on other traces",
			wantTrace, len(probeSpans))
	}
}

func spanSummary(spans []*tracepb.Span) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, fmt.Sprintf("%s[%s]", s.GetName(), s.GetKind()))
	}
	return out
}
