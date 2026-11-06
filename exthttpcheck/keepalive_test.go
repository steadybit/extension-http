// Copyright 2026 steadybit GmbH. All rights reserved.

package exthttpcheck

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steadybit/extension-http/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateHttpClient_KeepAlive(t *testing.T) {
	tests := []struct {
		name            string
		keepAlive       bool
		wantConnections int32
	}{
		{name: "keep-alive disabled opens a connection per request", keepAlive: false, wantConnections: 3},
		{name: "keep-alive enabled reuses the connection", keepAlive: true, wantConnections: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := config.Config.KeepAlive
			config.Config.KeepAlive = tt.keepAlive
			t.Cleanup(func() { config.Config.KeepAlive = previous })

			var connections atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			server.Start()
			t.Cleanup(server.Close)

			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			state := &HTTPCheckState{
				URL:               *target,
				Method:            http.MethodGet,
				ReadTimeout:       5 * time.Second,
				ConnectionTimeout: 5 * time.Second,
			}

			client := createHttpClient(state)
			for range 3 {
				req, err := createRequest(context.Background(), state)
				require.NoError(t, err)
				resp, err := client.Do(req)
				require.NoError(t, err)
				_, _ = io.Copy(io.Discard, resp.Body)
				require.NoError(t, resp.Body.Close())
			}

			assert.Equal(t, tt.wantConnections, connections.Load())
		})
	}
}
