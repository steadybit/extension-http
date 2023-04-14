/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"golang.org/x/exp/slices"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ExecutionRunData struct {
	stopTicker            chan bool                  // stores the stop channels for each execution
	jobs                  chan time.Time             // stores the jobs for each execution
	tickers               *time.Ticker               // stores the tickers for each execution, to be able to stop them
	metrics               chan action_kit_api.Metric // stores the metrics for each execution
	requestCounter        int                        // stores the number of requests for each execution
	requestSuccessCounter int                        // stores the number of successful requests for each execution
}

var (
	ExecutionRunDataMap = sync.Map{} //make(map[uuid.UUID]*ExecutionRunData)
)

type HttpCheckState struct {
	ExpectedStatusCodes      []int
	DelayBetweenRequestsInMS int64
	Timeout                  time.Time
	ResponsesContains        string
	SuccessRate              int
	MaxConcurrent            int
	NumberOfRequests         int
	RequestsPerSecond        int
	ReadTimeout              time.Duration
	ExecutionId              uuid.UUID
	Body                     string
	Url                      string
	Method                   string
	Headers                  map[string]string
	ConnectionTimeout        time.Duration
	FollowRedirects          bool
}

func prepare(request action_kit_api.PrepareActionRequestBody, state *HttpCheckState) (*action_kit_api.PrepareResult, error) {
	duration := toInt64(request.Config["duration"])
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(duration))
	expectedStatusCodes, err := resolveStatusCodeExpression(toString(request.Config["statusCode"]))
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve status codes")
		return nil, err
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.ResponsesContains = toString(request.Config["responsesContains"])
	state.SuccessRate = toInt(request.Config["successRate"])
	state.MaxConcurrent = toInt(request.Config["maxConcurrent"])
	state.NumberOfRequests = toInt(request.Config["numberOfRequests"])
	state.RequestsPerSecond = toInt(request.Config["requestsPerSecond"])
	state.ReadTimeout = time.Duration(toInt64(request.Config["readTimeout"])) * time.Second
	state.ExecutionId = request.ExecutionId
	state.Body = toString(request.Config["body"])
	state.Url = toString(request.Config["url"])
	state.Method = toString(request.Config["method"])
	state.ConnectionTimeout = time.Duration(toInt64(request.Config["connectTimeout"])) * time.Second
	state.FollowRedirects = toBool(request.Config["followRedirects"])
	state.Headers, err = toKeyValue(request, "headers")
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse headers")
		return nil, err
	}

	initExecutionRunData(state)
	executionRunData, err := loadExecutionRunData(state.ExecutionId)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	req, err := createRequest(state)

	// create worker pool
	for w := 1; w <= state.MaxConcurrent; w++ {
		go requestWorker(req, executionRunData.jobs, executionRunData.metrics, state)
	}
	return nil, nil
}

func loadExecutionRunData(executionId uuid.UUID) (*ExecutionRunData, error) {
	erd, ok := ExecutionRunDataMap.Load(executionId)
	if !ok {
		return nil, fmt.Errorf("failed to load execution run data")
	}
	executionRunData := erd.(*ExecutionRunData)
	return executionRunData, nil
}

func initExecutionRunData(state *HttpCheckState) {
	ExecutionRunDataMap.Store(state.ExecutionId, &ExecutionRunData{
		stopTicker:            make(chan bool),
		jobs:                  make(chan time.Time, state.MaxConcurrent),
		metrics:               make(chan action_kit_api.Metric, state.MaxConcurrent),
		requestCounter:        0,
		requestSuccessCounter: 0,
	})
}

func createRequest(state *HttpCheckState) (*http.Request, error) {
	var body io.Reader = nil
	if state.Body != "" {
		body = strings.NewReader(state.Body)
	}
	var method = "GET"
	if state.Method != "" {
		method = state.Method
	}

	request, err := http.NewRequest(strings.ToUpper(method), state.Url, body)
	if err != nil {
		for k, v := range state.Headers {
			request.Header.Add(k, v)
		}
	}
	return request, err
}

func requestWorker(req *http.Request, jobs chan time.Time, results chan action_kit_api.Metric, state *HttpCheckState) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: state.ConnectionTimeout,
		}).DialContext,
	}
	client := http.Client{Timeout: state.ReadTimeout, Transport: transport}

	if !state.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	for range jobs {
		var start = time.Now()
		var elapsed time.Duration

		trace := &httptrace.ClientTrace{
			WroteRequest: func(info httptrace.WroteRequestInfo) {
				start = time.Now()
			},
			GotFirstResponseByte: func() {
				elapsed = time.Since(start)
			},
		}

		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		log.Debug().Msgf("Requesting %s", req.URL.String())
		response, err := client.Do(req)

		executionRunData, err := loadExecutionRunData(state.ExecutionId)
		if err != nil {
			log.Error().Err(err).Msg("Failed to load execution run data")
			return
		}
		executionRunData.requestCounter++
		ExecutionRunDataMap.Store(state.ExecutionId, executionRunData)

		responseStatusWasExpected := false
		responseBodyWasSuccessful := true

		if err != nil {
			log.Error().Err(err).Msg("Failed to execute request")
			results <- action_kit_api.Metric{
				Metric: map[string]string{
					"url":   req.URL.String(),
					"error": err.Error(),
				},
				Name:      extutil.Ptr("response_time"),
				Value:     float64(time.Since(start).Milliseconds()),
				Timestamp: time.Now(),
			}
			responseStatusWasExpected = false
			return
		}
		log.Debug().Msgf("Got response %s", response.Status)
		responseStatusWasExpected = slices.Contains(state.ExpectedStatusCodes, response.StatusCode)
		metricMap := map[string]string{
			"url":                  req.URL.String(),
			"http_status":          strconv.Itoa(response.StatusCode),
			"expected_http_status": strconv.FormatBool(responseStatusWasExpected),
		}
		if state.ResponsesContains != "" {
			if response.Body == nil {
				metricMap["response_constraints_fulfilled"] = strconv.FormatBool(false)
				responseBodyWasSuccessful = false
			} else {
				bodyBytes, err := io.ReadAll(response.Body)
				if err != nil {
					log.Error().Err(err).Msg("Failed to read response body")
					metricMap["response_constraints_fulfilled"] = strconv.FormatBool(false)
					responseBodyWasSuccessful = false
				} else {
					bodyString := string(bodyBytes)
					metricMap["response_constraints_fulfilled"] = strconv.FormatBool(strings.Contains(bodyString, state.ResponsesContains))
					responseBodyWasSuccessful = true
				}
			}
		}
		if responseStatusWasExpected && responseBodyWasSuccessful {
			executionRunData.requestSuccessCounter++
		}
		ExecutionRunDataMap.Store(state.ExecutionId, executionRunData)

		metric := action_kit_api.Metric{
			Name:      extutil.Ptr("response_time"),
			Metric:    metricMap,
			Value:     float64(elapsed.Milliseconds()),
			Timestamp: time.Now(),
		}
		results <- metric
	}
}

func start(state *HttpCheckState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionId)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}
	executionRunData.tickers = time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)
	executionRunData.stopTicker = make(chan bool)

	now := time.Now()
	log.Debug().Msgf("Schedule first Request at %v", now)
	executionRunData.jobs <- now
	go func() {
		for {
			select {
			case <-executionRunData.stopTicker:
				log.Debug().Msg("Stop Request Scheduler")
				return
			case t := <-executionRunData.tickers.C:
				log.Debug().Msgf("Schedule Request at %v", t)
				executionRunData.jobs <- t
			}
		}
	}()
	ExecutionRunDataMap.Store(state.ExecutionId, executionRunData)
}

func retrieveLatestMetrics(executionId uuid.UUID) []action_kit_api.Metric {
	executionRunData, err := loadExecutionRunData(executionId)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return []action_kit_api.Metric{}
	}

	statusMetrics := make([]action_kit_api.Metric, 0, len(executionRunData.metrics))
	for {
		select {
		case metric, ok := <-executionRunData.metrics:
			if ok {
				log.Debug().Msgf("Status Metric: %v", metric)
				statusMetrics = append(statusMetrics, metric)
			} else {
				log.Debug().Msg("Channel closed")
				return statusMetrics
			}
		default:
			log.Debug().Msg("No metrics available")
			return statusMetrics
		}
	}
}

func stop(state *HttpCheckState) (*action_kit_api.StopResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionId)
	if err != nil {
		log.Debug().Err(err).Msg("Execution run data not found, stop was already called")
		return nil, nil
	}
	ticker := executionRunData.tickers
	if ticker != nil {
		ticker.Stop()
	}
	// non-blocking send
	select {
	case executionRunData.stopTicker <- true: // stop the ticker
		log.Trace().Msg("Stopped ticker")
	default:
		log.Debug().Msg("Ticker already stopped")
	}

	//get latest metrics
	latestMetrics := retrieveLatestMetrics(state.ExecutionId)
	// calculate the success rate
	successRate := float64(executionRunData.requestSuccessCounter) / float64(executionRunData.requestCounter) * 100
	log.Debug().Msgf("Success Rate: %f", successRate)
	ExecutionRunDataMap.Delete(state.ExecutionId)
	if successRate < float64(state.SuccessRate) {
		return extutil.Ptr(action_kit_api.StopResult{
			Metrics: extutil.Ptr(latestMetrics),
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%v) was below %v%", successRate, state.SuccessRate),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}), nil
	}
	return extutil.Ptr(action_kit_api.StopResult{
		Metrics: extutil.Ptr(latestMetrics),
	}), nil
}
