// Copyright 2026 steadybit GmbH. All rights reserved.

package exthttpcheck

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/extension-http/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connTrackingServer counts the connections a test server accepts and closes.
type connTrackingServer struct {
	*httptest.Server
	opened atomic.Int32
	closed atomic.Int32
}

func newConnTrackingServer(t *testing.T) *connTrackingServer {
	t.Helper()
	s := &connTrackingServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			s.opened.Add(1)
		case http.StateClosed, http.StateHijacked:
			s.closed.Add(1)
		default:
		}
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func setKeepAlive(t *testing.T, enabled bool) {
	t.Helper()
	previous := config.Config.KeepAlive
	config.Config.KeepAlive = enabled
	t.Cleanup(func() { config.Config.KeepAlive = previous })
}

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
			setKeepAlive(t, tt.keepAlive)
			server := newConnTrackingServer(t)

			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			state := &HTTPCheckState{
				URL:               *target,
				Method:            http.MethodGet,
				ReadTimeout:       5 * time.Second,
				ConnectionTimeout: 5 * time.Second,
			}

			client, _ := createHttpClient(state)
			for range 3 {
				req, err := createRequest(context.Background(), state)
				require.NoError(t, err)
				resp, err := client.Do(req)
				require.NoError(t, err)
				_, _ = io.Copy(io.Discard, resp.Body)
				require.NoError(t, resp.Body.Close())
			}

			assert.Equal(t, tt.wantConnections, server.opened.Load())
		})
	}
}

func TestHttpChecker_ShutdownReleasesKeptAliveConnection(t *testing.T) {
	setKeepAlive(t, true)
	server := newConnTrackingServer(t)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	state := &HTTPCheckState{
		ExecutionID:          uuid.New(),
		URL:                  *target,
		Method:               http.MethodGet,
		ReadTimeout:          5 * time.Second,
		ConnectionTimeout:    5 * time.Second,
		DelayBetweenRequests: time.Hour, // only the immediately scheduled first request runs
		MaxConcurrent:        1,
		ExpectedStatusCodes:  []string{"200"},
	}

	checker := newHttpChecker(context.Background(), state)
	checker.start()
	require.Eventually(t, func() bool {
		return checker.counters.success.Load()+checker.counters.failed.Load() == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(0), server.closed.Load(), "the kept-alive connection should still be open before shutdown")

	checker.shutdown()

	assert.Eventually(t, func() bool { return server.closed.Load() == server.opened.Load() }, 5*time.Second, 10*time.Millisecond,
		"shutdown must close the kept-alive connection")
}

func TestBandwidthChecker_StopReleasesKeptAliveConnections(t *testing.T) {
	setKeepAlive(t, true)
	server := newConnTrackingServer(t)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	checker := newBandwidthChecker(context.Background(), &BandwidthCheckState{
		ExecutionID:       uuid.New(),
		URL:               *target,
		ReadTimeout:       5 * time.Second,
		ConnectionTimeout: 5 * time.Second,
		MaxConcurrent:     2,
	})
	checker.start()
	require.Eventually(t, func() bool { return server.opened.Load() > 0 }, 5*time.Second, 10*time.Millisecond)

	checker.stop()

	assert.Eventually(t, func() bool { return server.closed.Load() == server.opened.Load() }, 5*time.Second, 10*time.Millisecond,
		"stop must close the workers' kept-alive connections")
}

func TestHttpChecker_KeepAliveReusesOneConnectionPerWorker(t *testing.T) {
	setKeepAlive(t, true)
	server := newConnTrackingServer(t)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	const concurrency = 3
	state := &HTTPCheckState{
		URL:               *target,
		Method:            http.MethodGet,
		ReadTimeout:       5 * time.Second,
		ConnectionTimeout: 5 * time.Second,
		MaxConcurrent:     concurrency,
	}
	client, _ := createHttpClient(state)

	// Several rounds of fully concurrent requests: each round needs `concurrency` connections
	// at once, and with an idle pool smaller than that, every round would open new ones.
	for range 5 {
		var wg sync.WaitGroup
		for range concurrency {
			wg.Go(func() {
				req, err := createRequest(context.Background(), state)
				if !assert.NoError(t, err) {
					return
				}
				resp, err := client.Do(req)
				if !assert.NoError(t, err) {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			})
		}
		wg.Wait()
	}

	assert.LessOrEqual(t, server.opened.Load(), int32(concurrency),
		"with keep-alive, connections must be reused up to the configured concurrency")
}
