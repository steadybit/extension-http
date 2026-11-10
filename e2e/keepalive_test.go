// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package e2e

import (
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/extension-http/exthttpcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connCountingServer is a plain HTTP server that counts the TCP connections it accepts and
// sees closed, to observe whether the extension reuses connections.
type connCountingServer struct {
	server *http.Server
	opened atomic.Int32
	closed atomic.Int32
}

func startConnCountingServer(t *testing.T, addr string) *connCountingServer {
	t.Helper()
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)

	s := &connCountingServer{}
	s.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				s.opened.Add(1)
			case http.StateClosed, http.StateHijacked:
				s.closed.Add(1)
			default:
			}
		},
	}
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("connection counting server failed: %v", err)
		}
	}()
	return s
}

func (s *connCountingServer) reset() {
	s.opened.Store(0)
	s.closed.Store(0)
}

func (s *connCountingServer) Close() error {
	return s.server.Close()
}

func testKeepAlive(server *connCountingServer, url string) func(*testing.T, *e2e.Minikube, *e2e.Extension) {
	const numberOfRequests = 10

	return func(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
		tests := []struct {
			name            string
			keepAlive       bool
			wantConnections int32
		}{
			{name: "keep-alive disabled opens a connection per request", keepAlive: false, wantConnections: numberOfRequests},
			{name: "keep-alive enabled reuses one connection", keepAlive: true, wantConnections: 1},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server.reset()

				config := map[string]any{
					"duration":         10000,
					"numberOfRequests": float64(numberOfRequests),
					"url":              url,
					"method":           "GET",
					"statusCode":       "200",
					"successRate":      100.0,
					"maxConcurrent":    1.0,
					"connectTimeout":   5000,
					"readTimeout":      5000,
					"headers":          []any{},
					"keepAlive":        tt.keepAlive,
				}

				action, err := e.RunAction(exthttpcheck.ActionIDFixedAmount, nil, config, nil)
				defer func() { _ = action.Cancel() }()
				require.NoError(t, err)
				require.NoError(t, action.Wait())

				assert.Equal(t, tt.wantConnections, server.opened.Load(), "unexpected number of connections for %d requests", numberOfRequests)

				// Once the check has stopped, no connection may be left open, including a kept-alive one.
				assert.Eventually(t, func() bool { return server.closed.Load() == server.opened.Load() }, 10*time.Second, 100*time.Millisecond,
					"connections left open after the check stopped: opened %d, closed %d", server.opened.Load(), server.closed.Load())
			})
		}
	}
}
