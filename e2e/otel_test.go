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

// otlpCollector is a minimal OTLP/HTTP trace receiver standing in for the
// collector an operator would point the extension at.
type otlpCollector struct {
	server *http.Server
	// port is resolved from the listener rather than fixed, so a developer
	// running a local collector on the default 4318 does not break the suite.
	port int

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
	c.port = listener.Addr().(*net.TCPAddr).Port

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
func otelExtraArgs(firstEnvIndex int, collector *otlpCollector) []string {
	env := []struct{ name, value string }{
		{"OTEL_EXPORTER_OTLP_ENDPOINT", fmt.Sprintf("http://host.minikube.internal:%d", collector.port)},
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

		// Group by trace and look for *any* trace that has both the action's
		// /prepare span and probe spans. Keying off a single "latest /prepare"
		// span would be racy: spans from earlier subtests can still arrive after
		// the reset, and a stale one would have no probes on its trace.
		type traceSpans struct {
			prepare *tracepb.Span
			probes  int
		}

		var matched *traceSpans
		// The batch processor flushes on its schedule delay, so give it a few
		// cycles after the action finished before concluding nothing arrived.
		if !assert.Eventually(t, func() bool {
			byTrace := map[string]*traceSpans{}
			for _, span := range collector.received() {
				id := hex.EncodeToString(span.GetTraceId())
				if byTrace[id] == nil {
					byTrace[id] = &traceSpans{}
				}
				switch {
				case span.GetKind() == tracepb.Span_SPAN_KIND_SERVER && strings.HasSuffix(span.GetName(), "/prepare"):
					byTrace[id].prepare = span
				case span.GetKind() == tracepb.Span_SPAN_KIND_CLIENT:
					byTrace[id].probes++
				}
			}
			for _, ts := range byTrace {
				if ts.prepare != nil && ts.probes > 0 {
					matched = ts
					return true
				}
			}
			return false
		}, 30*time.Second, 500*time.Millisecond) {
			// Evaluated here, not as an Eventually message argument: those are
			// built before the polling starts and would always report an empty
			// collector.
			t.Fatalf("no trace carried both the action's /prepare span and its probe spans; received: %v",
				spanSummary(collector.received()))
		}

		// The probes are issued by goroutines that outlive the Prepare request,
		// so a trace holding both is what proves the span context is carried
		// into the checker rather than lost when Prepare returns.
		assert.True(t, strings.HasPrefix(matched.prepare.GetName(), "POST /"),
			"server span should be named by method and route, got %q", matched.prepare.GetName())
	}
}

func spanSummary(spans []*tracepb.Span) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, fmt.Sprintf("%s[%s]", s.GetName(), s.GetKind()))
	}
	return out
}
