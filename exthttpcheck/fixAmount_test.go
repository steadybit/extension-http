/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	extension_kit "github.com/steadybit/extension-kit"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/stretchr/testify/assert"
)

func TestNewHTTPCheckActionFixedAmount_Prepare(t *testing.T) {
	action := httpCheckActionFixedAmount{}

	url, _ := url.Parse("https://steadybit.com")

	tests := []struct {
		name        string
		requestBody action_kit_api.PrepareActionRequestBody
		wantedError error
		wantedState *HTTPCheckState
	}{
		{
			name: "Should return config",
			requestBody: extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
				Config: map[string]interface{}{
					"action":            "prepare",
					"duration":          5000,
					"statusCode":        "200-209",
					"responsesContains": "test",
					"successRate":       100,
					"maxConcurrent":     10,
					"numberOfRequests":  20,
					"readTimeout":       5000,
					"body":              "test",
					"url":               "https://steadybit.com",
					"method":            "GET",
					"connectTimeout":    5000,
					"followRedirects":   true,
					"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
				},
				ExecutionId: uuid.New(),
			}),

			wantedState: &HTTPCheckState{
				ExpectedStatusCodes:      []string{"200", "201", "202", "203", "204", "205", "206", "207", "208", "209"},
				DelayBetweenRequestsInMS: 263,
				Timeout:                  time.Now(),
				ResponsesContains:        "test",
				SuccessRate:              100,
				MaxConcurrent:            10,
				NumberOfRequests:         20,
				RequestsPerSecond:        4,
				ReadTimeout:              time.Second * 5,
				ExecutionID:              uuid.New(),
				Body:                     "test",
				URL:                      *url,
				Method:                   "GET",
				Headers:                  map[string]string{"test": "test"},
				ConnectionTimeout:        time.Second * 5,
				FollowRedirects:          true,
			},
		}, {
			name: "Should return config and set RequestsPerSecond to 1 if less then one request per second",
			requestBody: extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
				Config: map[string]interface{}{
					"action":            "prepare",
					"duration":          5000,
					"statusCode":        "200",
					"responsesContains": "test",
					"successRate":       100,
					"maxConcurrent":     10,
					"numberOfRequests":  1,
					"readTimeout":       5000,
					"body":              "test",
					"url":               "https://steadybit.com",
					"method":            "GET",
					"connectTimeout":    5000,
					"followRedirects":   true,
					"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
				},
				ExecutionId: uuid.New(),
			}),

			wantedState: &HTTPCheckState{
				ExpectedStatusCodes:      []string{"200"},
				DelayBetweenRequestsInMS: 1000,
				Timeout:                  time.Now(),
				ResponsesContains:        "test",
				SuccessRate:              100,
				MaxConcurrent:            10,
				NumberOfRequests:         1,
				RequestsPerSecond:        1,
				ReadTimeout:              time.Second * 5,
				ExecutionID:              uuid.New(),
				Body:                     "test",
				URL:                      *url,
				Method:                   "GET",
				Headers:                  map[string]string{"test": "test"},
				ConnectionTimeout:        time.Second * 5,
				FollowRedirects:          true,
			},
		},
		{
			name: "Should return error for headers",
			requestBody: extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
				Config: map[string]interface{}{
					"action":           "prepare",
					"duration":         "5000",
					"numberOfRequests": 1,
					"headers":          "test:test",
					"statusCode":       "200",
				},
				ExecutionId: uuid.New(),
			}),

			wantedError: extension_kit.ToError("failed to interpret config value for headers as a key/value array", nil),
		}, {
			name: "Should return error missing duration",
			requestBody: action_kit_api.PrepareActionRequestBody{
				Config: map[string]interface{}{
					"action":            "prepare",
					"statusCode":        "200-209",
					"responsesContains": "test",
					"successRate":       100,
					"maxConcurrent":     10,
					"numberOfRequests":  5,
					"readTimeout":       5000,
					"body":              "test",
					"url":               "https://steadybit.com",
					"method":            "GET",
					"connectTimeout":    5000,
					"followRedirects":   true,
					"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
				},
				ExecutionId: uuid.New(),
			},

			wantedError: extension_kit.ToError("duration must be greater than 0", nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//Given
			state := action.NewEmptyState()
			request := tt.requestBody
			//When
			_, err := action.Prepare(context.Background(), &state, request)

			//Then
			if tt.wantedError != nil {
				assert.EqualError(t, err, tt.wantedError.Error())
			}
			if tt.wantedState != nil {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantedState.FollowRedirects, state.FollowRedirects)
				assert.Equal(t, tt.wantedState.ReadTimeout, state.ReadTimeout)
				assert.Equal(t, tt.wantedState.FollowRedirects, state.FollowRedirects)
				assert.Equal(t, tt.wantedState.ConnectionTimeout, state.ConnectionTimeout)
				assert.Equal(t, tt.wantedState.DelayBetweenRequestsInMS, state.DelayBetweenRequestsInMS)
				assert.Equal(t, tt.wantedState.ExpectedStatusCodes, state.ExpectedStatusCodes)
				assert.Equal(t, tt.wantedState.Headers, state.Headers)
				assert.Equal(t, tt.wantedState.MaxConcurrent, state.MaxConcurrent)
				assert.Equal(t, tt.wantedState.Method, state.Method)
				assert.Equal(t, tt.wantedState.NumberOfRequests, state.NumberOfRequests)
				assert.Equal(t, tt.wantedState.RequestsPerSecond, state.RequestsPerSecond)
				assert.Equal(t, tt.wantedState.ReadTimeout, state.ReadTimeout)
				assert.Equal(t, tt.wantedState.ResponsesContains, state.ResponsesContains)
				assert.Equal(t, tt.wantedState.SuccessRate, state.SuccessRate)
				assert.Equal(t, tt.wantedState.URL, state.URL)
				assert.NotNil(t, state.ExecutionID)
				assert.NotNil(t, state.Timeout)
				assert.EqualValues(t, tt.wantedState.Body, state.Body)
			}
		})
	}
}

func TestNewHTTPCheckActionFixedAmount_All_Success(t *testing.T) {
	// generate a test server so we can capture and inspect the request
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		_, _ = res.Write([]byte("this is a test response"))
	}))
	defer func() { testServer.Close() }()

	//prepare the action
	action := httpCheckActionFixedAmount{}
	state := action.NewEmptyState()
	prepareActionRequestBody := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"action":            "prepare",
			"duration":          2000,
			"statusCode":        "200-209",
			"responsesContains": "test",
			"successRate":       100,
			"maxConcurrent":     2,
			"numberOfRequests":  20,
			"readTimeout":       5000,
			"body":              "test",
			"url":               testServer.URL,
			"method":            "GET",
			"connectTimeout":    5000,
			"followRedirects":   true,
			"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})

	// Prepare
	prepareResult, err := action.Prepare(context.Background(), &state, prepareActionRequestBody)
	assert.NoError(t, err)
	assert.Nil(t, prepareResult)
	assert.Greater(t, state.DelayBetweenRequestsInMS, extutil.ToUInt64(0))

	checker, err := action.getHttpChecker(state.ExecutionID)
	assert.NoError(t, err)
	assert.NotNil(t, checker)

	// Start
	startResult, err := action.Start(context.Background(), &state)
	assert.NoError(t, err)
	assert.Nil(t, startResult)

	// Status
	statusResult, err := action.Status(context.Background(), &state)
	assert.NoError(t, err)
	assert.NotNil(t, statusResult.Metrics)

	time.Sleep(2 * time.Second)

	// Status completed
	statusResult, err = action.Status(context.Background(), &state)
	assert.NoError(t, err)
	assert.Equal(t, statusResult.Completed, true)
	assert.Greater(t, len(*statusResult.Metrics), 0)
	assert.Equal(t, checker.counters.started.Load(), uint64(20))

	// Stop
	stopResult, err := action.Stop(context.Background(), &state)
	assert.NoError(t, err)
	assert.NotNil(t, stopResult.Metrics)
	assert.Nil(t, stopResult.Error)
	assert.Equal(t, checker.counters.success.Load(), uint64(20))
}

func TestNewHTTPCheckActionFixedAmount_All_Failure(t *testing.T) {
	// generate a test server so we can capture and inspect the request
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(404)
	}))
	defer func() { testServer.Close() }()

	//prepare the action
	action := httpCheckActionFixedAmount{}
	state := action.NewEmptyState()
	prepareActionRequestBody := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"action":            "prepare",
			"duration":          1000,
			"statusCode":        "200-209",
			"responsesContains": "test",
			"successRate":       100,
			"maxConcurrent":     10,
			"numberOfRequests":  2,
			"readTimeout":       5000,
			"body":              "test",
			"url":               testServer.URL,
			"method":            "GET",
			"connectTimeout":    5000,
			"followRedirects":   true,
			"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})

	// Prepare
	prepareResult, err := action.Prepare(context.Background(), &state, prepareActionRequestBody)
	assert.NoError(t, err)
	assert.Nil(t, prepareResult)
	assert.Greater(t, state.DelayBetweenRequestsInMS, extutil.ToUInt64(0))

	// Start
	startResult, err := action.Start(context.Background(), &state)
	assert.NoError(t, err)
	assert.Nil(t, startResult)

	// Status
	statusResult, err := action.Status(context.Background(), &state)
	assert.NoError(t, err)
	assert.NotNil(t, statusResult.Metrics)

	time.Sleep(1100 * time.Millisecond)

	// Status completed
	statusResult, err = action.Status(context.Background(), &state)
	assert.NoError(t, err)
	assert.Equal(t, statusResult.Completed, true)
	assert.Greater(t, len(*statusResult.Metrics), 0)

	executionRunData, err := action.getHttpChecker(state.ExecutionID)
	assert.NoError(t, err)
	assert.Greater(t, executionRunData.counters.started.Load(), uint64(0))

	// Stop
	stopResult, err := action.Stop(context.Background(), &state)
	assert.NoError(t, err)
	assert.NotNil(t, stopResult.Metrics)
	assert.NotNil(t, stopResult.Error)
	assert.Equal(t, stopResult.Error.Title, "Success Rate (0.00%) was below 100%")
	assert.Equal(t, executionRunData.counters.success.Load(), uint64(0))
}

func TestNewHTTPCheckActionFixedAmount_start_directly(t *testing.T) {
	// write receive timestamps to the channel to check the delay between requests
	var receivedRequests = make(chan time.Time)

	// generate a test server so we can capture and inspect the request
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		receivedRequests <- time.Now()
		res.WriteHeader(200)
	}))
	defer func() { testServer.Close() }()

	//prepare the action
	action := httpCheckActionFixedAmount{}
	state := action.NewEmptyState()
	prepareActionRequestBody := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"action":            "prepare",
			"duration":          2000,
			"statusCode":        "200-209",
			"responsesContains": "test",
			"successRate":       100,
			"maxConcurrent":     1,
			"numberOfRequests":  3,
			"readTimeout":       5000,
			"body":              "test",
			"url":               testServer.URL,
			"method":            "GET",
			"connectTimeout":    5000,
			"followRedirects":   true,
			"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})
	_, err := action.Prepare(context.Background(), &state, prepareActionRequestBody)
	assert.NoError(t, err)

	// start
	now := time.Now()
	_, _ = action.Start(context.Background(), &state)

	// first request is executed immediately, check with quite big tolerance to avoid flakiness
	firstRequest := <-receivedRequests
	assert.WithinDuration(t, now, firstRequest, 400*time.Millisecond)

	// second request is executed after 1 second (now + 1 * (2000 / (3 - 1)))
	secondRequest := <-receivedRequests
	assert.WithinDuration(t, now.Add(1*time.Second), secondRequest, 400*time.Millisecond)

	// third request is executed after 2 second (now + 2 * (2000 / (3 - 1)))
	thirdRequest := <-receivedRequests
	assert.WithinDuration(t, now.Add(2*time.Second), thirdRequest, 400*time.Millisecond)
}

// Use pprof to visualize memory usage after run:
// > go tool pprof -http=:8080 mem.pprof
func TestNewHTTPCheckActionFixedAmount_start_multiples(t *testing.T) {
	t.Skip("manual pprof test")
	zerolog.SetGlobalLevel(zerolog.Disabled)

	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
	}))
	defer func() { testServer.Close() }()

	//prepare the action
	request := extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: map[string]interface{}{
			"action":            "prepare",
			"duration":          50,
			"statusCode":        "200-209",
			"responsesContains": "test",
			"successRate":       100,
			"maxConcurrent":     5,
			"numberOfRequests":  2,
			"readTimeout":       5000,
			"body":              "test",
			"url":               testServer.URL,
			"method":            "GET",
			"connectTimeout":    5000,
			"followRedirects":   true,
			"headers":           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		},
		ExecutionId: uuid.New(),
	})

	memProfile, err := os.Create("mem.pprof")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = pprof.WriteHeapProfile(memProfile)
		_ = memProfile.Close()
	}()

	var m runtime.MemStats
	action := httpCheckActionFixedAmount{}
	// sequential execution to simulate a long-running extension
	for i := 0; i < 1000; i++ {
		if i%100 == 0 {
			runtime.ReadMemStats(&m)
			fmt.Printf("%3v - Alloc = %v MiB, Heap Objects = %v, GCs = %v\n", i, m.HeapAlloc/1024/1024, m.HeapObjects, m.NumGC)
		}

		request.ExecutionId = uuid.New()
		state := action.NewEmptyState()
		_, err = action.Prepare(context.Background(), &state, request)
		assert.NoError(t, err)
		_, err = action.Start(context.Background(), &state)
		assert.NoError(t, err)

		time.Sleep(time.Duration(extutil.ToInt64(request.Config["duration"])) * time.Millisecond)

		_, err = action.Status(context.Background(), &state)
		assert.NoError(t, err)
		_, err = action.Stop(context.Background(), &state)
		assert.NoError(t, err)
	}
}
