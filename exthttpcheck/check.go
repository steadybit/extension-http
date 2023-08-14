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
	ExpectedStatusCodes      []int
	DelayBetweenRequestsInMS int64
	Timeout                  time.Time
	ResponsesContains        string
	SuccessRate              int
	MaxConcurrent            int
	NumberOfRequests         uint64
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
	expectedStatusCodes, err := resolveStatusCodeExpression(extutil.ToString(request.Config["statusCode"]))
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve status codes")
		return nil, err
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.ResponsesContains = extutil.ToString(request.Config["responsesContains"])
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])
	state.NumberOfRequests = extutil.ToUInt64(request.Config["numberOfRequests"])
	state.ReadTimeout = time.Duration(extutil.ToInt64(request.Config["readTimeout"])) * time.Millisecond
	state.ExecutionID = request.ExecutionId
	state.Body = extutil.ToString(request.Config["body"])
	state.Method = extutil.ToString(request.Config["method"])
	state.ConnectionTimeout = time.Duration(extutil.ToInt64(request.Config["connectTimeout"])) * time.Millisecond
	state.FollowRedirects = extutil.ToBool(request.Config["followRedirects"])
	state.Headers, err = extutil.ToKeyValue(request.Config, "headers")
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse headers")
		return nil, err
	}

	urlString, ok := request.Config["url"]
	if !ok {
		return nil, fmt.Errorf("URL is missing")
	}
	u, err := url.Parse(extutil.ToString(urlString))
	if err != nil {
		log.Error().Err(err).Msg("URL could not be parsed missing")
		return nil, err
	}
	state.URL = *u

	initExecutionRunData(state)
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	// create worker pool
	for w := 1; w <= state.MaxConcurrent; w++ {
		go requestWorker(executionRunData, state, checkEnded)
	}
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
	saveExecutionRunData(state.ExecutionID, &ExecutionRunData{
		stopTicker:            make(chan bool),
		jobs:                  make(chan time.Time, state.MaxConcurrent),
		metrics:               make(chan action_kit_api.Metric, state.MaxConcurrent),
		requestCounter:        atomic.Uint64{},
		requestSuccessCounter: atomic.Uint64{},
	})
}

func saveExecutionRunData(executionID uuid.UUID, executionRunData *ExecutionRunData) {
	ExecutionRunDataMap.Store(executionID, executionRunData)
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

func requestWorker(executionRunData *ExecutionRunData, state *HTTPCheckState, checkEnded func(executionRunData *ExecutionRunData, state *HTTPCheckState) bool) {
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

	for range executionRunData.jobs {
		if !checkEnded(executionRunData, state) {
			var start = time.Now()
			var elapsed time.Duration

      // see seems to break a configured proxy. maybe we can use it in the future and configure the proxy here
			trace := &httptrace.ClientTrace{
				WroteRequest: func(info httptrace.WroteRequestInfo) {
					start = time.Now()
				},
				GotFirstResponseByte: func() {
					elapsed = time.Since(start)
				},
			}

			responseStatusWasExpected := false
			responseBodyWasSuccessful := true

			req, err := createRequest(state)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create request")
				executionRunData.metrics <- action_kit_api.Metric{
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

			req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
			log.Debug().Msgf("Requesting %s", req.URL.String())
			response, err := client.Do(req)

			executionRunData.requestCounter.Add(1)
			//ExecutionRunDataMap.Store(state.ExecutionID, executionRunData)

			if err != nil {
				log.Error().Err(err).Msg("Failed to execute request")
				executionRunData.metrics <- action_kit_api.Metric{
					Metric: map[string]string{
						"url":   req.URL.String(),
						"error": err.Error(),
					},
					Name:      extutil.Ptr("response_time"),
					Value:     float64(time.Since(start).Milliseconds()),
					Timestamp: time.Now(),
				}
				responseStatusWasExpected = false
			} else {
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
					executionRunData.requestSuccessCounter.Add(1)
				}
				//ExecutionRunDataMap.Store(state.ExecutionID, executionRunData)

				metric := action_kit_api.Metric{
					Name:      extutil.Ptr("response_time"),
					Metric:    metricMap,
					Value:     float64(elapsed.Milliseconds()),
					Timestamp: time.Now(),
				}
				executionRunData.metrics <- metric
			}
		}
	}
}

func start(state *HTTPCheckState) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
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
	stopTickers(executionRunData)

	//get latest metrics
	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	// calculate the success rate
	//Uint64.Load(&executionRunData.requestCounter)
	successRate := float64(executionRunData.requestSuccessCounter.Load()) / float64(executionRunData.requestCounter.Load()) * 100
	log.Debug().Msgf("Success Rate: %v%%", successRate)
	ExecutionRunDataMap.Delete(state.ExecutionID)
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

func stopTickers(executionRunData *ExecutionRunData) {
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
}
