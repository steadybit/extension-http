// Copyright 2025 steadybit GmbH. All rights reserved.

package exthttpcheck

import (
	"crypto/tls"
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

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
)

type checkEndedFn func(checker *httpChecker) bool

type counters struct {
	started atomic.Uint64 // stores the number of requests for each execution
	success atomic.Uint64 // stores the number of successful requests for each execution
	failed  atomic.Uint64 // stores the number of failed requests for each execution
}

type httpChecker struct {
	wg          sync.WaitGroup
	work        chan struct{} // stores the work for each execution
	stopSignal  chan struct{} // stores the stop signal for each execution
	metrics     chan action_kit_api.Metric
	shouldStop  func() bool // function to determine whether the check should end
	counters    counters    // stores the counters for each execution
	tickerDelay time.Duration
}

func newHttpChecker(state *HTTPCheckState, fnStop checkEndedFn) *httpChecker {
	checker := &httpChecker{
		work:        make(chan struct{}, state.MaxConcurrent),
		stopSignal:  make(chan struct{}, 1),
		metrics:     make(chan action_kit_api.Metric, state.RequestsPerSecond*2),
		counters:    counters{},
		tickerDelay: time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond,
	}

	if fnStop != nil {
		checker.shouldStop = func() bool {
			return fnStop(checker)
		}
	} else {
		checker.shouldStop = func() bool {
			return false
		}
	}

	go func(c *httpChecker) {
		checker.startWorkers(state)
		c.wg.Wait()
		close(c.metrics)
	}(checker)

	return checker
}

func (c *httpChecker) startWorkers(state *HTTPCheckState) {
	for w := 1; w <= state.MaxConcurrent; w++ {
		c.wg.Go(func() {
			client := createHttpClient(state)

			for range c.work {
				if c.shouldStop() {
					break
				}

				if c.performRequest(state, client) {
					return
				}
			}
		})
	}
}

func (c *httpChecker) start() {
	ticker := time.NewTicker(c.tickerDelay)

	log.Debug().Msgf("Schedule first Request at %v", time.Now())
	c.work <- struct{}{}

	go func() {
		defer func() {
			close(c.work)
		}()

		for {
			select {
			case t := <-ticker.C:
				log.Debug().Msgf("Schedule Request at %v", t)
				select {
				case c.work <- struct{}{}:
				case <-c.stopSignal:
					return
				default:
					log.Debug().Msgf("Dropping tick at %v, all workers busy", t)
				}

			case <-c.stopSignal:
				return
			}
		}
	}()
}

func (c *httpChecker) performRequest(state *HTTPCheckState, client http.Client) bool {
	req, err := createRequest(state)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create request")
		c.onError(req, err, 0, false)
		return true
	}

	tracer := newRequestTracer()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &tracer.ClientTrace))

	if log.Logger.GetLevel() == zerolog.TraceLevel {
		log.Trace().Any("headers", req.Header).Str("body", state.Body).Msgf("Requesting %s %s", req.Method, req.URL.String())
	} else {
		log.Debug().Msgf("Requesting %s %s", req.Method, req.URL.String())
	}

	started := time.Now()
	c.counters.started.Add(1)

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
	return false
}

func createHttpClient(state *HTTPCheckState) http.Client {
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
	return client
}

func (c *httpChecker) onError(req *http.Request, err error, value int64, responseStatusWasExpected bool) {
	c.metrics <- action_kit_api.Metric{
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
		c.counters.success.Add(1)
	} else {
		c.counters.failed.Add(1)
	}
}

func (c *httpChecker) onResponse(req *http.Request, res *http.Response, tracer *requestTracer, responseStatusWasExpected bool, responseBodyWasSuccessful bool, responseTimeWasSuccessful bool) {
	c.metrics <- action_kit_api.Metric{
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
		c.counters.success.Add(1)
	} else {
		c.counters.failed.Add(1)
	}
}

func (c *httpChecker) stop() {
	c.stopSignal <- struct{}{}
	c.wg.Wait()
}

func (c *httpChecker) getLatestMetrics() []action_kit_api.Metric {
	metrics := make([]action_kit_api.Metric, 0, len(c.metrics))
	for {
		select {
		case metric, ok := <-c.metrics:
			if ok {
				log.Debug().Msgf("Status Metric: %v", metric)
				metrics = append(metrics, metric)
			} else {
				log.Trace().Msg("Channel closed")
				return metrics
			}
		default:
			log.Trace().Msg("No metrics available")
			return metrics
		}
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
