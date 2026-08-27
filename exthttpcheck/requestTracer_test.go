// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A delay injected during connection setup (before the request is written) must
// be reflected in responseTime — this is what a WroteRequest→GotFirstResponseByte
// window would miss.
func Test_responseTime_includes_connection_setup(t *testing.T) {
	const setupDelay = 200 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tracer := newRequestTracer()

	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)

	// Inject the delay before the connection is established, mimicking a
	// transparent proxy that stalls at connect/handshake time.
	base := tracer.ClientTrace
	base.GetConn = func(hostPort string) {
		if tracer.connectionStart.IsZero() {
			tracer.connectionStart = time.Now()
		}
		time.Sleep(setupDelay)
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &base))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.False(t, tracer.connectionStart.IsZero(), "connectionStart must be captured")
	require.False(t, tracer.firstByteReceived.IsZero(), "firstByteReceived must be captured")
	require.GreaterOrEqual(t, tracer.responseTime(), setupDelay,
		"responseTime must include the connection-setup delay")
}
