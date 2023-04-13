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

// Make sure Action implements all required interfaces
var (
	stopTicker = sync.Map{} //map[uuid.UUID]chan bool{}                  // stores the stop channels for each execution
	jobs       = sync.Map{} //map[uuid.UUID]chan time.Time{}             // stores the jobs for each execution
	tickers    = sync.Map{} //map[uuid.UUID]*time.Ticker{}               // stores the tickers for each execution, to be able to stop them
	metrics    = sync.Map{} //map[uuid.UUID]chan action_kit_api.Metric{} // stores the metrics for each execution

	requestCounter        = sync.Map{} //map[uuid.UUID]int{} // stores the number of requests for each execution
	requestSuccessCounter = sync.Map{} //map[uuid.UUID]int{} // stores the number of successful requests for each execution
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

	metrics.Store(state.ExecutionId, make(chan action_kit_api.Metric, state.MaxConcurrent))

	req, err := createRequest(state)

	// create job channel
	jobs.Store(state.ExecutionId, make(chan time.Time, state.MaxConcurrent))
	// create metrics result channel
	metrics.Store(state.ExecutionId, make(chan action_kit_api.Metric, state.MaxConcurrent))
	// create worker pool
	for w := 1; w <= state.MaxConcurrent; w++ {
		jobs, ok := jobs.Load(state.ExecutionId)
		metrics, ok := metrics.Load(state.ExecutionId)
		if ok {
			go requestWorker(req, jobs.(chan time.Time), metrics.(chan action_kit_api.Metric), state)
		}
	}
	return nil, nil
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

		cnt, ok := requestCounter.Load(state.ExecutionId)
		if !ok {
			cnt = toInt(cnt) + 1
			requestCounter.Store(state.ExecutionId, cnt)
		}

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
			cnt, _ = requestSuccessCounter.Load(state.ExecutionId)
			requestSuccessCounter.Store(state.ExecutionId, toInt(cnt)+1)
		}
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
	tickers.Store(state.ExecutionId, time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS)*time.Millisecond))
	stopTicker.Store(state.ExecutionId, make(chan bool))
	now := time.Now()
	log.Debug().Msgf("Schedule first Request at %v", now)
	j, ok := jobs.Load(state.ExecutionId)
	if ok {
		j.(chan time.Time) <- now
	}
	go func() {
		for {
			st, _ := stopTicker.Load(state.ExecutionId)
			tick, _ := tickers.Load(state.ExecutionId)
			select {
			case <-st.(chan bool):
				return
			case t := <-tick.(*time.Ticker).C:
				log.Debug().Msgf("Schedule Request at %v", t)
				j, ok := jobs.Load(state.ExecutionId)
				if ok {
					j.(chan time.Time) <- t
				}
			}
		}
	}()
}

func retrieveLatestMetrics(executionId uuid.UUID) []action_kit_api.Metric {
	m, _ := metrics.Load(executionId)
	statusMetrics := make([]action_kit_api.Metric, 0, len(m.(chan action_kit_api.Metric)))
	for {
		m, _ := metrics.Load(executionId)
		select {
		case metric, ok := <-m.(chan action_kit_api.Metric):
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
	return statusMetrics
}

func stop(state *HttpCheckState) (*action_kit_api.StopResult, error) {
	t, _ := tickers.Load(state.ExecutionId)
	if t != nil {
		t.(*time.Ticker).Stop()
	}
	s, _ := stopTicker.Load(state.ExecutionId)
	s.(chan bool) <- true // stop the ticker

	//get latest metrics
	latestMetrics := retrieveLatestMetrics(state.ExecutionId)
	// calculate the success rate
	rsc, _ := requestSuccessCounter.Load(state.ExecutionId)
	rc, _ := requestCounter.Load(state.ExecutionId)
	successRate := float64(rsc.(int)) / float64(rc.(int)) * 100
	log.Debug().Msgf("Success Rate: %f", successRate)
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
