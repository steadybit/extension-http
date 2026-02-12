// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpChecker_ExecutesExactlyMaxRequests(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(200)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	state := &HTTPCheckState{
		MaxConcurrent:            1,
		NumberOfRequests:         5,
		RequestsPerSecond:        50,
		DelayBetweenRequestsInMS: 20,
		ExpectedStatusCodes:      []string{"200"},
		URL:                      *serverURL,
		Method:                   "GET",
		ReadTimeout:              5 * time.Second,
		ConnectionTimeout:        5 * time.Second,
	}

	checker := newHttpChecker(state)
	checker.start()

	assert.Eventually(t, func() bool {
		return checker.counters.started.Load() >= 5
	}, 5*time.Second, 10*time.Millisecond)

	checker.shutdown(false)

	assert.Equal(t, int32(5), requestCount.Load())
	assert.Equal(t, uint64(5), checker.counters.started.Load())
}

func TestHttpChecker_ShutdownCancelsInFlightRequests(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	state := &HTTPCheckState{
		MaxConcurrent:            2,
		NumberOfRequests:         0,
		RequestsPerSecond:        10,
		DelayBetweenRequestsInMS: 100,
		ExpectedStatusCodes:      []string{"200"},
		URL:                      *serverURL,
		Method:                   "GET",
		ReadTimeout:              5 * time.Second,
		ConnectionTimeout:        5 * time.Second,
	}

	checker := newHttpChecker(state)
	checker.start()

	time.Sleep(200 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		checker.shutdown(true)
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown() did not return in time, in-flight requests were not cancelled")
	}

	assert.Zero(t, checker.counters.failed.Load(), "cancelled requests should not be counted as failures")
}

func TestHttpChecker_ShouldStop(t *testing.T) {
	tests := []struct {
		name        string
		maxRequests uint64
		requested   uint64
		want        bool
	}{
		{
			name:        "unlimited requests never stops",
			maxRequests: 0,
			requested:   100,
			want:        false,
		},
		{
			name:        "not yet reached",
			maxRequests: 10,
			requested:   5,
			want:        false,
		},
		{
			name:        "exactly reached",
			maxRequests: 10,
			requested:   10,
			want:        true,
		},
		{
			name:        "exceeded",
			maxRequests: 10,
			requested:   15,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checker := &httpChecker{
				ctx:         ctx,
				cancel:      cancel,
				maxRequests: tt.maxRequests,
			}
			checker.counters.requested.Store(tt.requested)

			assert.Equal(t, tt.want, checker.isCompleted())
		})
	}
}

func TestCreateRequest_SetsMethodHeadersAndBody(t *testing.T) {
	serverURL, _ := url.Parse("https://example.com/test")
	state := &HTTPCheckState{
		URL:     *serverURL,
		Method:  "POST",
		Body:    `{"key":"value"}`,
		Headers: map[string]string{"Content-Type": "application/json", "X-Custom": "test"},
	}

	req, err := createRequest(context.Background(), state)
	require.NoError(t, err)

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "https://example.com/test", req.URL.String())
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "test", req.Header.Get("X-Custom"))
	assert.NotNil(t, req.Body)
}

func TestCreateRequest_DefaultsToGet(t *testing.T) {
	serverURL, _ := url.Parse("https://example.com")
	state := &HTTPCheckState{
		URL: *serverURL,
	}

	req, err := createRequest(context.Background(), state)
	require.NoError(t, err)

	assert.Equal(t, "GET", req.Method)
	assert.Nil(t, req.Body)
}

func TestCreateRequest_PropagatesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	serverURL, _ := url.Parse("https://example.com")
	state := &HTTPCheckState{
		URL: *serverURL,
	}

	req, err := createRequest(ctx, state)
	require.NoError(t, err)
	assert.Equal(t, context.Canceled, req.Context().Err())
}
