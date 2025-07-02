// Copyright 2025 steadybit GmbH. All rights reserved.

package exthttpcheck

import (
	"crypto/tls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type checkEndedFn func(checker *httpChecker) bool

type httpChecker struct {
	work              chan struct{}              // stores the work for each execution
	ticker            *time.Ticker               // stores the ticker for each execution, to be able to stop them
	metrics           chan action_kit_api.Metric // stores the metrics for each execution
	counterReqStarted atomic.Uint64              // stores the number of requests for each execution
	counterReqSuccess atomic.Uint64              // stores the number of successful requests for each execution
	counterReqFailed  atomic.Uint64              // stores the number of failed requests for each execution
	shouldEnd         func() bool                //
	tickerDelay       time.Duration
}

func newHttpChecker(state *HTTPCheckState, checkEnded checkEndedFn) *httpChecker {
	checker := &httpChecker{
		work:              make(chan struct{}, state.MaxConcurrent),
		metrics:           make(chan action_kit_api.Metric, state.RequestsPerSecond*2),
		counterReqStarted: atomic.Uint64{},
		counterReqSuccess: atomic.Uint64{},
		tickerDelay:       time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond,
	}
	if checkEnded != nil {
		checker.shouldEnd = func() bool {
			return checkEnded(checker)
		}
	}

	//start workers doing the actual requests
	go func() {
		defer func() {
			close(checker.metrics)
		}()

		var wg sync.WaitGroup
		for w := 1; w <= state.MaxConcurrent; w++ {
			wg.Add(1)
			go func() {
				checker.performRequests(state)
				wg.Done()
			}()
		}
		wg.Wait()
	}()

	return checker
}

func (c *httpChecker) start() {
	c.ticker = time.NewTicker(c.tickerDelay)

	log.Debug().Msgf("Schedule first Request at %v", time.Now())
	c.work <- struct{}{}

	go func() {
		defer func() {
			close(c.work)
			log.Trace().Msg("Stopped Request Scheduler")
		}()

		for t := range c.ticker.C {
			log.Debug().Msgf("Schedule Request at %v", t)
			c.work <- struct{}{}
		}
	}()
}

func (c *httpChecker) performRequests(state *HTTPCheckState) {
	// restrict idle connections, as all will point to one target
	transport := &http.Transport{
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   true,
		DialContext:         (&net.Dialer{Timeout: state.ConnectionTimeout}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: state.InsecureSkipVerify,
		},
	}
	client := http.Client{Timeout: state.ReadTimeout, Transport: transport}

	if !state.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	for range c.work {
		if c.shouldEnd != nil && c.shouldEnd() {
			break
		}

		req, err := createRequest(state)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create request")
			c.onError(req, err, 0, false)
			return
		}

		tracer := newRequestTracer()
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &tracer.ClientTrace))

		if log.Logger.GetLevel() == zerolog.TraceLevel {
			log.Trace().Any("headers", req.Header).Str("body", state.Body).Msgf("Requesting %s %s", req.Method, req.URL.String())
		} else {
			log.Debug().Msgf("Requesting %s %s", req.Method, req.URL.String())
		}

		started := time.Now()
		c.counterReqStarted.Add(1)

		response, err := client.Do(req)
		if err != nil {
			log.Error().Err(err).Msg("Failed to execute request")
			now := time.Now()

			responseStatusWasExpected := slices.Contains(state.ExpectedStatusCodes, "error")
			c.onError(req, err, now.Sub(started).Milliseconds(), responseStatusWasExpected)
		} else {
			var bodyBytes []byte
			var bodyErr error
			if response.Body != nil {
				if bodyBytes, bodyErr = io.ReadAll(response.Body); bodyErr != nil {
					log.Error().Err(err).Msg("Failed to read response body")
				}
			}

			if log.Logger.GetLevel() == zerolog.TraceLevel {
				log.Trace().Str("status", response.Status).Bytes("body", bodyBytes).Any("headers", response.Header).Msgf("Got response for %s %s", req.Method, req.URL.String())
			} else {
				log.Debug().Str("status", response.Status).Int("body-size", len(bodyBytes)).Msgf("Got response for %s %s", req.Method, req.URL.String())
			}

			responseStatusWasExpected := slices.Contains(state.ExpectedStatusCodes, strconv.Itoa(response.StatusCode))
			responseBodyWasSuccessful := true
			if state.ResponsesContains != "" {
				if len(bodyBytes) == 0 || bodyErr != nil {
					responseBodyWasSuccessful = false
				} else {
					responseBodyWasSuccessful = strings.Contains(string(bodyBytes), state.ResponsesContains)
				}
			}

			var responseTimeWasSuccessful bool
			switch state.ResponseTimeMode {
			case "SHORTER_THAN":
				responseTimeWasSuccessful = tracer.responseTime() <= *state.ResponseTime
			case "LONGER_THAN":
				responseTimeWasSuccessful = tracer.responseTime() >= *state.ResponseTime
			default:
				responseTimeWasSuccessful = true
			}

			c.onResponse(req, response, tracer, responseStatusWasExpected, responseBodyWasSuccessful, responseTimeWasSuccessful)

			if response.Body != nil {
				_ = response.Body.Close()
			}
		}
	}
}

func (c *httpChecker) onError(req *http.Request, err error, value int64, responseStatusWasExpected bool) {
	metric := action_kit_api.Metric{
		Metric: map[string]string{
			"url":                  req.URL.String(),
			"error":                err.Error(),
			"expected_http_status": strconv.FormatBool(responseStatusWasExpected),
		},
		Name:      extutil.Ptr("response_time"),
		Value:     float64(value),
		Timestamp: time.Now(),
	}

	if responseStatusWasExpected {
		c.counterReqSuccess.Add(1)
	} else {
		c.counterReqFailed.Add(1)
	}
	c.metrics <- metric
}

func (c *httpChecker) onResponse(req *http.Request, res *http.Response, tracer *requestTracer, responseStatusWasExpected bool, responseBodyWasSuccessful bool, responseTimeWasSuccessful bool) {
	metric := action_kit_api.Metric{
		Name: extutil.Ptr("response_time"),
		Metric: map[string]string{
			"url":                                 req.URL.String(),
			"http_status":                         strconv.Itoa(res.StatusCode),
			"expected_http_status":                strconv.FormatBool(responseStatusWasExpected),
			"response_constraints_fulfilled":      strconv.FormatBool(responseBodyWasSuccessful),
			"response_time_constraints_fulfilled": strconv.FormatBool(responseTimeWasSuccessful),
		},
		Value:     float64(tracer.responseTime().Milliseconds()),
		Timestamp: tracer.firstByteReceived,
	}

	if responseStatusWasExpected && responseBodyWasSuccessful && responseTimeWasSuccessful {
		c.counterReqSuccess.Add(1)
	} else {
		c.counterReqFailed.Add(1)
	}

	c.metrics <- metric
}

func (c *httpChecker) stop() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
}

func createRequest(state *HTTPCheckState) (*http.Request, error) {
	var body io.Reader
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
