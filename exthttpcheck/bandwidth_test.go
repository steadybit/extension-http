// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBitrate(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"1000bit", 1000, false},
		{"1kbit", 1000, false},
		{"10kbit", 10000, false},
		{"1mbit", 1_000_000, false},
		{"10mbit", 10_000_000, false},
		{"1gbit", 1_000_000_000, false},
		{"1024kbit", 1_024_000, false},
		{"1bps", 1, false},
		{"1kbps", 1000, false},
		{"1mbps", 1_000_000, false},
		{"1gbps", 1_000_000_000, false},
		{"100", 100, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"10xbit", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseBitrate(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBandwidthCheckAction_Describe(t *testing.T) {
	action := NewHTTPCheckActionBandwidth()
	desc := action.Describe()

	assert.Equal(t, ActionIDBandwidth, desc.Id)
	assert.Equal(t, "HTTP (Bandwidth)", desc.Label)
	assert.Equal(t, action_kit_api.Check, desc.Kind)
	assert.Equal(t, action_kit_api.TimeControlExternal, desc.TimeControl)
	assert.NotEmpty(t, desc.Parameters)
}

func TestBandwidthCheckAction_Prepare_MissingURL(t *testing.T) {
	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"minBandwidth": "1mbit",
		},
		ExecutionId: uuid.New(),
	}

	_, err := action.Prepare(context.Background(), &state, request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL is missing")
}

func TestBandwidthCheckAction_Prepare_MissingBandwidthThresholds(t *testing.T) {
	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":     "http://example.com",
			"headers": []interface{}{},
		},
		ExecutionId: uuid.New(),
	}

	result, err := action.Prepare(context.Background(), &state, request)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Title, "At least one of minimum or maximum bandwidth must be specified")
}

func TestBandwidthCheckAction_Prepare_InvalidMinGreaterThanMax(t *testing.T) {
	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":          "http://example.com",
			"minBandwidth": "10mbit",
			"maxBandwidth": "1mbit",
			"headers":      []interface{}{},
		},
		ExecutionId: uuid.New(),
	}

	result, err := action.Prepare(context.Background(), &state, request)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Title, "Minimum bandwidth cannot be greater than maximum bandwidth")
}

func TestBandwidthCheckAction_Prepare_Success(t *testing.T) {
	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	execID := uuid.New()
	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":               "http://example.com",
			"minBandwidth":      "1mbit",
			"maxBandwidth":      "100mbit",
			"successRate":       80,
			"connectTimeout":    5000,
			"readTimeout":       5000,
			"followRedirects":   true,
			"headers":           []interface{}{},
		},
		ExecutionId: execID,
	}

	result, err := action.Prepare(context.Background(), &state, request)
	assert.NoError(t, err)
	assert.Nil(t, result)

	assert.Equal(t, "http://example.com", state.URL.String())
	assert.Equal(t, int64(1_000_000), state.MinBandwidthBps)
	assert.Equal(t, int64(100_000_000), state.MaxBandwidthBps)
	assert.Equal(t, 80, state.SuccessRate)
	assert.Equal(t, execID, state.ExecutionID)

	// Clean up
	bandwidthCheckers.Delete(execID)
}

func TestBandwidthCheckAction_FullCycle(t *testing.T) {
	// Create a test server that returns some data
	dataSize := 1024 * 100 // 100KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, dataSize)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", dataSize))
		_, _ = w.Write(data)
	}))
	defer server.Close()

	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	execID := uuid.New()
	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":               server.URL,
			"minBandwidth":      "1kbit", // Very low threshold to ensure success
			"successRate":       100,
			"connectTimeout":    5000,
			"readTimeout":       5000,
			"followRedirects":   true,
			"maxConcurrent":     10,
			"headers":           []interface{}{},
		},
		ExecutionId: execID,
	}

	// Prepare
	result, err := action.Prepare(context.Background(), &state, request)
	require.NoError(t, err)
	require.Nil(t, result)

	// Start
	startResult, err := action.Start(context.Background(), &state)
	require.NoError(t, err)
	require.Nil(t, startResult)

	// Wait for some requests to complete
	time.Sleep(500 * time.Millisecond)

	// Status - triggers window metric emission and reset
	statusResult, err := action.Status(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, statusResult)
	assert.False(t, statusResult.Completed)
	// Should have one metric from the window
	require.NotNil(t, statusResult.Metrics)
	assert.Len(t, *statusResult.Metrics, 1)

	// Stop - emits final window metric, should succeed
	stopResult, err := action.Stop(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, stopResult)
	assert.Nil(t, stopResult.Error)
}

func TestBandwidthCheckAction_BigFileDownload(t *testing.T) {
	// Simulate a big file served at a controlled rate.
	// The server writes 128KB chunks with 100ms sleeps between them,
	// yielding ~1.28 MB/s ≈ 10.24 Mbps of application-level throughput.
	const chunkSize = 128 * 1024 // 128KB per chunk
	const chunkDelay = 100 * time.Millisecond
	const totalChunks = 50 // 50 chunks × 128KB = 6.4 MB, takes ~5s
	const totalSize = chunkSize * totalChunks

	// Expected bandwidth: 128KB / 0.1s = 1.28 MB/s = 10.24 Mbps
	expectedMbps := float64(chunkSize*8) / chunkDelay.Seconds() / 1_000_000

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", totalSize))
		chunk := make([]byte, chunkSize)
		for i := 0; i < totalChunks; i++ {
			_, err := w.Write(chunk)
			if err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(chunkDelay)
		}
	}))
	defer server.Close()

	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	execID := uuid.New()
	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":               server.URL,
			"minBandwidth":      "1kbit",
			"successRate":       100,
			"connectTimeout":    10000,
			"readTimeout":       10000,
			"followRedirects":   true,
			"maxConcurrent":     1,
			"headers":           []interface{}{},
		},
		ExecutionId: execID,
	}

	// Prepare
	result, err := action.Prepare(context.Background(), &state, request)
	require.NoError(t, err)
	require.Nil(t, result)

	// Start
	startResult, err := action.Start(context.Background(), &state)
	require.NoError(t, err)
	require.Nil(t, startResult)

	// Simulate the platform calling Status every second across multiple windows.
	// The download takes ~5s, so we should get several windows with bandwidth data.
	var windowMetrics []action_kit_api.Metric
	for i := 0; i < 6; i++ {
		time.Sleep(1 * time.Second)

		statusResult, err := action.Status(context.Background(), &state)
		require.NoError(t, err)
		require.NotNil(t, statusResult)

		if statusResult.Metrics != nil {
			for _, m := range *statusResult.Metrics {
				t.Logf("Window %d: bandwidth=%.2f Mbps, bytes=%s, duration=%sms",
					i+1, m.Value, m.Metric["bytes_downloaded"], m.Metric["duration_ms"])
				windowMetrics = append(windowMetrics, m)
			}
		}
	}

	// Stop
	stopResult, err := action.Stop(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, stopResult)
	if stopResult.Metrics != nil {
		for _, m := range *stopResult.Metrics {
			t.Logf("Final window: bandwidth=%.2f Mbps, bytes=%s, duration=%sms",
				m.Value, m.Metric["bytes_downloaded"], m.Metric["duration_ms"])
			windowMetrics = append(windowMetrics, m)
		}
	}

	// We must have collected at least 3 windows worth of data
	require.GreaterOrEqual(t, len(windowMetrics), 3,
		"Expected at least 3 measurement windows for a multi-second download")

	// Each window (except possibly the first and last) should report bandwidth
	// reasonably close to the expected rate. We allow a generous tolerance of 50%
	// to account for timing jitter, but this would still catch a bug where bandwidth
	// is reported as a fraction (e.g. 10x too low).
	lowThreshold := expectedMbps * 0.5
	for i, m := range windowMetrics {
		bwStr := m.Metric["bandwidth"]
		bw, parseErr := strconv.ParseFloat(bwStr, 64)
		require.NoError(t, parseErr)

		t.Logf("Window %d: reported=%.2f Mbps, expected=~%.2f Mbps, threshold=%.2f Mbps",
			i, bw, expectedMbps, lowThreshold)

		assert.Greaterf(t, bw, lowThreshold,
			"Window %d bandwidth %.2f Mbps is way below expected %.2f Mbps", i, bw, expectedMbps)
	}
}

func TestBandwidthCheckAction_NonSuccessStatusCode(t *testing.T) {
	// Server returns 403 Forbidden with a small body (simulates user-agent blocking)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><body>Forbidden</body></html>"))
	}))
	defer server.Close()

	action := &httpCheckActionBandwidth{}
	state := action.NewEmptyState()

	execID := uuid.New()
	request := action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"url":               server.URL,
			"minBandwidth":      "1kbit",
			"successRate":       100,
			"connectTimeout":    5000,
			"readTimeout":       5000,
			"followRedirects":   true,
			"maxConcurrent":     2,
			"headers":           []interface{}{},
		},
		ExecutionId: execID,
	}

	// Prepare
	result, err := action.Prepare(context.Background(), &state, request)
	require.NoError(t, err)
	require.Nil(t, result)

	// Start
	startResult, err := action.Start(context.Background(), &state)
	require.NoError(t, err)
	require.Nil(t, startResult)

	// Let requests hit the 403 server
	time.Sleep(1500 * time.Millisecond)

	// Status — should report errors, not bandwidth
	statusResult, err := action.Status(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, statusResult)

	if statusResult.Metrics != nil {
		for _, m := range *statusResult.Metrics {
			t.Logf("Metric: bandwidth=%.2f Mbps, errors=%s, requests=%s",
				m.Value, m.Metric["error_count"], m.Metric["request_count"])
			errorCount, _ := strconv.ParseInt(m.Metric["error_count"], 10, 64)
			requestCount, _ := strconv.ParseInt(m.Metric["request_count"], 10, 64)
			assert.Greater(t, errorCount, int64(0), "Expected errors from 403 responses")
			assert.Equal(t, int64(0), requestCount, "Expected no successful requests")
		}
	}

	// Stop — should fail because all windows had errors
	stopResult, err := action.Stop(context.Background(), &state)
	require.NoError(t, err)
	require.NotNil(t, stopResult)
	assert.NotNil(t, stopResult.Error, "Expected failure due to 403 responses")
}
