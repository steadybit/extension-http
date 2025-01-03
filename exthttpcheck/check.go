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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ExecutionRunData struct {
	stopTicker            chan bool                  // stores the stop channels for each execution
	jobs                  chan time.Time             // stores the jobs for each execution
	tickers               *time.Ticker               // stores the tickers for each execution, to be able to stop them
	metrics               chan action_kit_api.Metric // stores the metrics for each execution
	requestCounter        atomic.Uint64              // stores the number of requests for each execution
	requestSuccessCounter atomic.Uint64              // stores the number of successful requests for each execution
}

var (
	ExecutionRunDataMap = sync.Map{} //make(map[uuid.UUID]*ExecutionRunData)
)

type HTTPCheckState struct {
	ExpectedStatusCodes      []string
	DelayBetweenRequestsInMS uint64
	Timeout                  time.Time
	ResponsesContains        string
	SuccessRate              int
	ResponseTimeMode         string
	ResponseTime             *time.Duration
	MaxConcurrent            int
	NumberOfRequests         uint64
	RequestsPerSecond        uint64
	ReadTimeout              time.Duration
	ExecutionID              uuid.UUID
	Body                     string
	URL                      url.URL
	Method                   string
	Headers                  map[string]string
	ConnectionTimeout        time.Duration
	FollowRedirects          bool
}

func prepare(request action_kit_api.PrepareActionRequestBody, state *HTTPCheckState, checkEnded func(executionRunData *ExecutionRunData, state *HTTPCheckState) bool) (*action_kit_api.PrepareResult, error) {
	duration := extutil.ToInt64(request.Config["duration"])
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(duration))
	expectedStatusCodes, statusCodeErr := resolveStatusCodeExpression(extutil.ToString(request.Config["statusCode"]))
	if statusCodeErr != nil {
		return &action_kit_api.PrepareResult{
			Error: statusCodeErr,
		}, nil
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.ResponsesContains = extutil.ToString(request.Config["responsesContains"])
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	state.ResponseTimeMode = extutil.ToString(request.Config["responseTimeMode"])
	state.ResponseTime = extutil.Ptr(time.Duration(extutil.ToInt64(request.Config["responseTime"])) * time.Millisecond)
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])
	state.NumberOfRequests = extutil.ToUInt64(request.Config["numberOfRequests"])
	state.ReadTimeout = time.Duration(extutil.ToInt64(request.Config["readTimeout"])) * time.Millisecond
	state.ExecutionID = request.ExecutionId
	state.Body = extutil.ToString(request.Config["body"])
	state.Method = extutil.ToString(request.Config["method"])
	state.ConnectionTimeout = time.Duration(extutil.ToInt64(request.Config["connectTimeout"])) * time.Millisecond
	state.FollowRedirects = extutil.ToBool(request.Config["followRedirects"])
	var err error
	state.Headers, err = extutil.ToKeyValue(request.Config, "headers")
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse headers")
		return nil, err
	}

	urlString, ok := request.Config["url"]
	if !ok {
		return nil, fmt.Errorf("URL is missing")
	}
	parsedUrl, err := url.Parse(extutil.ToString(urlString))
	if err != nil {
		log.Error().Err(err).Msg("URL could not be parsed missing")
		return nil, err
	}
	state.URL = *parsedUrl

	initExecutionRunData(state)
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	// create worker pool, and close metrics once all workers are done
	go func() {
		var wg sync.WaitGroup
		for w := 1; w <= state.MaxConcurrent; w++ {
			wg.Add(1)
			go requestWorker(executionRunData, state, checkEnded, &wg)
		}
		wg.Wait()
		close(executionRunData.metrics)
	}()
	return nil, nil
}

func loadExecutionRunData(executionID uuid.UUID) (*ExecutionRunData, error) {
	erd, ok := ExecutionRunDataMap.Load(executionID)
	if !ok {
		return nil, fmt.Errorf("failed to load execution run data")
	}
	executionRunData := erd.(*ExecutionRunData)
	return executionRunData, nil
}

func initExecutionRunData(state *HTTPCheckState) {
	ExecutionRunDataMap.Store(state.ExecutionID, &ExecutionRunData{
		stopTicker:            make(chan bool),
		jobs:                  make(chan time.Time, state.MaxConcurrent),
		metrics:               make(chan action_kit_api.Metric, state.RequestsPerSecond),
		requestCounter:        atomic.Uint64{},
		requestSuccessCounter: atomic.Uint64{},
	})
}

func createRequest(state *HTTPCheckState) (*http.Request, error) {
	var body io.Reader = nil
	if state.Body != "" {
		body = strings.NewReader(state.Body)
	}
	var method = "GET"
	if state.Method != "" {
		method = state.Method
	}

	request, err := http.NewRequest(strings.ToUpper(method), state.URL.String(), body)
	if err == nil {
		for k, v := range state.Headers {
			request.Header.Add(k, v)
		}
	}
	return request, err
}

func requestWorker(executionRunData *ExecutionRunData, state *HTTPCheckState, checkEnded func(executionRunData *ExecutionRunData, state *HTTPCheckState) bool, wg *sync.WaitGroup) {
	// restrict idle connections, as all will point to one target
	transport := &http.Transport{
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   true,
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

	for range executionRunData.jobs {
		if !checkEnded(executionRunData, state) {
			var started = time.Now()
			var requestWritten time.Time
			var ended time.Time

			// see seems to break a configured proxy. maybe we can use it in the future and configure the proxy here
			trace := &httptrace.ClientTrace{
				WroteRequest: func(info httptrace.WroteRequestInfo) {
					requestWritten = time.Now()
				},
				GotFirstResponseByte: func() {
					ended = time.Now()
				},
			}

			responseStatusWasExpected := false

			req, err := createRequest(state)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create request")
				now := time.Now()
				executionRunData.metrics <- action_kit_api.Metric{
					Metric: map[string]string{
						"url":   req.URL.String(),
						"error": err.Error(),
					},
					Name:      extutil.Ptr("response_time"),
					Value:     float64(now.Sub(started).Milliseconds()),
					Timestamp: now,
				}
				return
			}

			req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
			log.Debug().Msgf("Requesting %s", req.URL.String())
			response, err := client.Do(req)

			executionRunData.requestCounter.Add(1)

			if err != nil {
				log.Error().Err(err).Msg("Failed to execute request")
				now := time.Now()
				responseStatusWasExpected = slices.Contains(state.ExpectedStatusCodes, "error")
				executionRunData.metrics <- action_kit_api.Metric{
					Metric: map[string]string{
						"url":                  req.URL.String(),
						"error":                err.Error(),
						"expected_http_status": strconv.FormatBool(responseStatusWasExpected),
					},
					Name:      extutil.Ptr("response_time"),
					Value:     float64(now.Sub(started).Milliseconds()),
					Timestamp: now,
				}
				if responseStatusWasExpected {
					executionRunData.requestSuccessCounter.Add(1)
				}
			} else {
				responseBodyWasSuccessful := true
				responseTimeWasSuccessful := true
				responseTimeValue := float64(ended.Sub(requestWritten).Milliseconds())
				log.Debug().Msgf("Got response %s", response.Status)
				responseStatusWasExpected = slices.Contains(state.ExpectedStatusCodes, strconv.Itoa(response.StatusCode))
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
							responseConstraintFulfilled := strings.Contains(bodyString, state.ResponsesContains)
							metricMap["response_constraints_fulfilled"] = strconv.FormatBool(responseConstraintFulfilled)
							responseBodyWasSuccessful = responseConstraintFulfilled
						}
					}
				}
				if state.ResponseTimeMode == "SHORTER_THAN" {
					if responseTimeValue > float64(state.ResponseTime.Milliseconds()) {
						responseTimeWasSuccessful = false
					}
					metricMap["response_time_constraints_fulfilled"] = strconv.FormatBool(responseTimeWasSuccessful)
				}
				if state.ResponseTimeMode == "LONGER_THAN" {
					if responseTimeValue < float64(state.ResponseTime.Milliseconds()) {
						responseTimeWasSuccessful = false
					}
					metricMap["response_time_constraints_fulfilled"] = strconv.FormatBool(responseTimeWasSuccessful)
				}

				if responseStatusWasExpected && responseBodyWasSuccessful && responseTimeWasSuccessful {
					executionRunData.requestSuccessCounter.Add(1)
				}

				metric := action_kit_api.Metric{
					Name:      extutil.Ptr("response_time"),
					Metric:    metricMap,
					Value:     responseTimeValue,
					Timestamp: ended,
				}
				executionRunData.metrics <- metric
			}
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
		}
	}
	wg.Done()
}

func start(state *HTTPCheckState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}
	executionRunData.tickers = time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)

	now := time.Now()
	log.Debug().Msgf("Schedule first Request at %v", now)
	executionRunData.jobs <- now
	go func() {
		for {
			select {
			case _, ok := <-executionRunData.stopTicker:
				if !ok {
					log.Debug().Msg("Stop Request Scheduler")
					// close jobs channel to free worker goroutines
					close(executionRunData.jobs)
					executionRunData.tickers.Stop()
					ExecutionRunDataMap.Delete(state.ExecutionID)
					log.Trace().Msg("Stopped Request Scheduler")
					return
				}
			case t := <-executionRunData.tickers.C:
				log.Debug().Msgf("Schedule Request at %v", t)
				executionRunData.jobs <- t
			}
		}
	}()
	ExecutionRunDataMap.Store(state.ExecutionID, executionRunData)
}

func retrieveLatestMetrics(metrics chan action_kit_api.Metric) []action_kit_api.Metric {
	statusMetrics := make([]action_kit_api.Metric, 0, len(metrics))
	for {
		select {
		case metric, ok := <-metrics:
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

func stop(state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Debug().Err(err).Msg("Execution run data not found, stop was already called")
		return nil, nil
	}

	// Close ticker to stop sending requests
	if executionRunData.stopTicker != nil {
		close(executionRunData.stopTicker)
	}

	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	// calculate the success rate
	successRate := float64(executionRunData.requestSuccessCounter.Load()) / float64(executionRunData.requestCounter.Load()) * 100
	log.Debug().Msgf("Success Rate: %v%%", successRate)
	if successRate < float64(state.SuccessRate) {
		log.Info().Msgf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate)
		return extutil.Ptr(action_kit_api.StopResult{
			Metrics: extutil.Ptr(latestMetrics),
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}), nil
	}
	log.Info().Msgf("Success Rate (%.2f%%) was above/equal %v%%", successRate, state.SuccessRate)
	return extutil.Ptr(action_kit_api.StopResult{
		Metrics: extutil.Ptr(latestMetrics),
	}), nil
}
