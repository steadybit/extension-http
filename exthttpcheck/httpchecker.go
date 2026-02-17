// Copyright 2025 steadybit GmbH. All rights reserved.

package exthttpcheck

import (
	"context"
	"crypto/tls"
	"errors"
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

type counters struct {
	requested atomic.Uint64 // stores the number of requests for each execution
	started   atomic.Uint64 // stores the number of requests for each execution
	success   atomic.Uint64 // stores the number of successful requests for each execution
	failed    atomic.Uint64 // stores the number of failed requests for each execution
}

type httpChecker struct {
	wg          sync.WaitGroup
	work        chan struct{} // stores the work for each execution
	ctx         context.Context
	ctxCancel   context.CancelFunc
	metrics     chan action_kit_api.Metric
	counters    counters // stores the counters for each execution
	tickerDelay time.Duration
	maxRequests uint64
	logger      zerolog.Logger
	httpClient  http.Client
}

func newHttpChecker(state *HTTPCheckState) *httpChecker {
	ctx, cancel := context.WithCancel(context.Background())
	checker := &httpChecker{
		work:        make(chan struct{}, state.MaxConcurrent),
		ctx:         ctx,
		ctxCancel:   cancel,
		metrics:     make(chan action_kit_api.Metric, state.RequestsPerSecond*2),
		counters:    counters{},
		tickerDelay: time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond,
		maxRequests: state.NumberOfRequests,
		logger:      log.With().Str("executionId", state.ExecutionID.String()).Logger(),
		httpClient:  createHttpClient(state),
	}

	go checker.startWorkers(state)

	return checker
}

func (c *httpChecker) startWorkers(state *HTTPCheckState) {

	for w := 1; w <= state.MaxConcurrent; w++ {
		c.wg.Go(func() {
			for range c.work {
				if req, err := createRequest(c.ctx, state); err == nil {
					c.performRequest(req, state)
				} else {
					c.logger.Error().Err(err).Msg("Failed to create request")
				}
			}
		})
	}
}

func (c *httpChecker) start() {
	ticker := time.NewTicker(c.tickerDelay)

	c.work <- struct{}{}
	c.counters.requested.Add(1)
	c.logger.Debug().Msgf("Scheduled first Request at %v", time.Now())

	go func() {
		defer func() {
			ticker.Stop()
			close(c.work)
		}()

		for {
			select {
			case t := <-ticker.C:
				select {
				case c.work <- struct{}{}:
					counter := c.counters.requested.Add(1)
					c.logger.Debug().Msgf("Scheduled Request at %v", t)
					if c.maxRequests > 0 && counter >= c.maxRequests {
						return
					}
				default:
					c.logger.Debug().Msgf("Dropping tick at %v, all workers busy", t)
				}

			case <-c.ctx.Done():
				return
			}
		}
	}()
}

func (c *httpChecker) performRequest(req *http.Request, state *HTTPCheckState) {
	tracer := newRequestTracer()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &tracer.ClientTrace))

	if c.logger.GetLevel() == zerolog.TraceLevel {
		c.logger.Trace().Any("headers", req.Header).Str("body", state.Body).Msgf("Requesting %s %s", req.Method, req.URL.String())
	} else {
		c.logger.Debug().Msgf("Requesting %s %s", req.Method, req.URL.String())
	}

	started := time.Now()
	c.counters.started.Add(1)

	response, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		c.logger.Error().Err(err).Msg("Failed to execute request")
		now := time.Now()

		responseStatusWasExpected := slices.Contains(state.ExpectedStatusCodes, "error")
		c.onError(req, err, float64(now.Sub(started).Milliseconds()), responseStatusWasExpected)
	} else {
		var bodyBytes []byte
		var bodyErr error
		if response.Body != nil {
			if bodyBytes, bodyErr = io.ReadAll(response.Body); bodyErr != nil {
				c.logger.Error().Err(err).Msg("Failed to read response body")
			}
		}

		if c.logger.GetLevel() == zerolog.TraceLevel {
			c.logger.Trace().Str("status", response.Status).Bytes("body", bodyBytes).Any("headers", response.Header).Msgf("Got response for %s %s", req.Method, req.URL.String())
		} else {
			c.logger.Debug().Str("status", response.Status).Int("body-size", len(bodyBytes)).Msgf("Got response for %s %s", req.Method, req.URL.String())
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

func (c *httpChecker) onError(req *http.Request, err error, responseTime float64, responseStatusWasExpected bool) {
	c.metrics <- action_kit_api.Metric{
		Metric: map[string]string{
			"url":                  req.URL.String(),
			"error":                err.Error(),
			"expected_http_status": strconv.FormatBool(responseStatusWasExpected),
		},
		Name:      extutil.Ptr("response_time"),
		Value:     responseTime,
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

func (c *httpChecker) shutdown() {
	c.ctxCancel()
	c.wg.Wait()
}

func (c *httpChecker) getLatestMetrics() []action_kit_api.Metric {
	metrics := make([]action_kit_api.Metric, 0, len(c.metrics))
	for {
		select {
		case metric := <-c.metrics:
			c.logger.Trace().Msgf("Status Metric: %v", metric)
			metrics = append(metrics, metric)
		default:
			c.logger.Trace().Msg("No more metrics available")
			return metrics
		}
	}
}

func createRequest(ctx context.Context, state *HTTPCheckState) (*http.Request, error) {
	var body io.Reader
	if state.Body != "" {
		body = strings.NewReader(state.Body)
	}
	var method = "GET"
	if state.Method != "" {
		method = state.Method
	}

	request, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), state.URL.String(), body)
	if err == nil {
		for k, v := range state.Headers {
			request.Header.Add(k, v)
		}
	}
	return request, err
}
