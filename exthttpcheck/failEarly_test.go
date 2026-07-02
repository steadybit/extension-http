/*
 * Copyright 2026 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/stretchr/testify/assert"
)

func TestSuccessRateUnreachable(t *testing.T) {
	tests := []struct {
		name        string
		failed      uint64
		expected    uint64
		successRate uint64
		want        bool
	}{
		{name: "100%: no failure allowed, none yet", failed: 0, expected: 10, successRate: 100, want: false},
		{name: "100%: first failure makes it unreachable", failed: 1, expected: 10, successRate: 100, want: true},
		{name: "80% of 10: 2 failures still ok", failed: 2, expected: 10, successRate: 80, want: false},
		{name: "80% of 10: 3 failures unreachable", failed: 3, expected: 10, successRate: 80, want: true},
		{name: "0% success rate never fails early", failed: 10, expected: 10, successRate: 0, want: false},
		{name: "unknown expected total never fails early", failed: 5, expected: 0, successRate: 100, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, successRateUnreachable(tt.failed, tt.expected, tt.successRate))
		})
	}
}

func TestFixedAmount_FailEarly(t *testing.T) {
	// Server always responds with an error status, so every request fails.
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(500)
	}))
	defer testServer.Close()

	action := httpCheckActionFixedAmount{}
	state := action.NewEmptyState()
	request := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]any{
			"duration":         2000,
			"statusCode":       "200-209",
			"successRate":      100,
			"maxConcurrent":    1,
			"numberOfRequests": 4,
			"readTimeout":      5000,
			"url":              testServer.URL,
			"method":           "GET",
			"connectTimeout":   5000,
			"followRedirects":  true,
			"failEarly":        true,
			"headers":          []any{map[string]any{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})

	_, err := action.Prepare(context.Background(), &state, request)
	assert.NoError(t, err)
	assert.True(t, state.FailEarly)
	assert.Equal(t, uint64(4), state.ExpectedRequests)

	_, err = action.Start(context.Background(), &state)
	assert.NoError(t, err)

	// Poll status until the fail-early condition triggers (should happen well before the 2s duration,
	// since with successRate=100 the very first failed request makes the rate unreachable).
	var statusResult *action_kit_api.StatusResult
	for i := 0; i < 20; i++ {
		statusResult, err = action.Status(context.Background(), &state)
		assert.NoError(t, err)
		if statusResult.Error != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	assert.NotNil(t, statusResult.Error, "expected the check to fail early")
	assert.True(t, statusResult.Completed)
	assert.Contains(t, statusResult.Error.Title, "can no longer reach 100%")

	_, _ = action.Stop(context.Background(), &state)
}

func TestFixedAmount_FailEarlyDisabledByDefault(t *testing.T) {
	action := httpCheckActionFixedAmount{}
	state := action.NewEmptyState()
	request := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]any{
			"duration":         1000,
			"statusCode":       "200-209",
			"successRate":      100,
			"maxConcurrent":    1,
			"numberOfRequests": 2,
			"readTimeout":      5000,
			"url":              "http://example.com",
			"method":           "GET",
			"connectTimeout":   5000,
			"headers":          []any{map[string]any{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})

	_, err := action.Prepare(context.Background(), &state, request)
	assert.NoError(t, err)
	assert.False(t, state.FailEarly) // non-breaking default
	_, _ = action.Stop(context.Background(), &state)
}
