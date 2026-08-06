// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extbuild"
)

type bandwidthChecker struct {
	// Window aggregation
	windowMu              sync.Mutex
	windowStartTime       time.Time
	windowBytesDownloaded int64
	windowRequestCount    int64
	windowErrorCount      int64
	// windowStatusCounts counts every response received in the window by HTTP status code,
	// successful or not, so the metric can report the status code for all calls.
	windowStatusCounts map[int]int64
	// windowTransportErrors counts, by error text, every call that never received a response at
	// all (request build failure, connect/DNS/TLS/timeout failures, or a body read failure).
	windowTransportErrors map[string]int64

	// Counters for success rate calculation (per window)
	counterWindowSuccess atomic.Uint64
	counterWindowFailed  atomic.Uint64

	// Total requests that completed successfully and that errored across the whole
	// run, used to detect a target that is failing every request regardless of
	// throughput, without penalising long downloads still in flight at stop.
	counterRequestsCompleted atomic.Uint64
	counterRequestsErrored   atomic.Uint64

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	state  *BandwidthCheckState
}

var bandwidthCheckers = sync.Map{}

func newBandwidthChecker(state *BandwidthCheckState) *bandwidthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	c := &bandwidthChecker{
		ctx:    ctx,
		cancel: cancel,
		state:  state,
	}
	c.resetWindowLocked()
	return c
}

// resetWindowLocked clears the current window's counters and maps to start a fresh
// measurement window. Callers must hold windowMu, except the constructor, where the
// checker isn't reachable by any other goroutine yet.
func (c *bandwidthChecker) resetWindowLocked() {
	c.windowStartTime = time.Now()
	c.windowBytesDownloaded = 0
	c.windowRequestCount = 0
	c.windowErrorCount = 0
	c.windowStatusCounts = make(map[int]int64)
	c.windowTransportErrors = make(map[string]int64)
}

func (c *bandwidthChecker) start() {
	c.windowMu.Lock()
	c.resetWindowLocked()
	c.windowMu.Unlock()

	// Start workers that continuously perform requests without delay
	for w := 1; w <= c.state.MaxConcurrent; w++ {
		go c.performBandwidthRequests()
	}

	log.Debug().Msgf("Started %d bandwidth workers", c.state.MaxConcurrent)
}

func (c *bandwidthChecker) stop() {
	// Cancelling the context stops the worker loop and aborts any in-flight request or
	// body read, so workers blocked on a slow or stalled endpoint return promptly instead
	// of leaking their goroutine and connection. Context cancellation propagates through
	// net/http to a blocked response.Body.Read, which is why no overall client timeout
	// (which would cap legitimate long downloads) is needed.
	c.cancel()
}

func (c *bandwidthChecker) performBandwidthRequests() {
	transport := &http.Transport{
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   true,
		DialContext:         (&net.Dialer{Timeout: c.state.ConnectionTimeout}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.state.InsecureSkipVerify,
		},
		// For bandwidth testing, we need to allow long downloads
		// ResponseHeaderTimeout controls time to wait for response headers
		ResponseHeaderTimeout: c.state.ReadTimeout,
	}
	// Don't set client.Timeout - it would limit the entire request including body read
	// For bandwidth testing, we want to allow large downloads to complete
	client := http.Client{Transport: transport}

	if !c.state.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	for c.ctx.Err() == nil {
		req, err := http.NewRequestWithContext(c.ctx, "GET", c.state.URL.String(), nil)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create bandwidth request")
			c.recordTransportError(err)
			continue
		}

		req.Header.Set("User-Agent", "steadybit/extension-http:"+extbuild.GetSemverVersionStringOrUnknown())
		for k, v := range c.state.Headers {
			req.Header.Add(k, v)
		}

		startTime := time.Now()
		response, err := client.Do(req)
		if err != nil {
			if c.ctx.Err() != nil {
				return // stopped: the request was cancelled, exit without recording a spurious error
			}
			log.Error().Err(err).Msg("Failed to execute bandwidth request")
			c.recordTransportError(err)
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			log.Error().Msgf("Unexpected HTTP status %d for bandwidth request to %s", response.StatusCode, c.state.URL.String())
			c.recordBadStatus(response.StatusCode)
			continue
		}

		// Recorded for every successful response too, so the metric can report the status
		// code for all calls rather than only the ones that failed.
		c.recordStatusCode(response.StatusCode)

		// Read the body in chunks, updating statistics incrementally
		// This ensures long downloads contribute to metrics before completing
		buf := make([]byte, 32*1024) // 32KB chunks
		var totalBytesRead int64
		var readErr error
		for {
			n, err := response.Body.Read(buf)
			if n > 0 {
				totalBytesRead += int64(n)
				c.recordBytes(int64(n))
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				readErr = err
				break
			}
		}
		_ = response.Body.Close()

		if readErr != nil {
			if c.ctx.Err() != nil {
				return // stopped: the body read was cancelled, exit without recording a spurious error
			}
			log.Error().Err(readErr).Msg("Failed to read response body")
			c.recordTransportError(readErr)
			continue
		}

		elapsed := time.Since(startTime)
		c.recordRequestCompleted()

		log.Trace().Msgf("Request completed: %d bytes in %v", totalBytesRead, elapsed)
	}
}

func (c *bandwidthChecker) recordBytes(bytesDownloaded int64) {
	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowBytesDownloaded += bytesDownloaded
}

func (c *bandwidthChecker) recordRequestCompleted() {
	c.counterRequestsCompleted.Add(1)

	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowRequestCount++
}

func (c *bandwidthChecker) recordStatusCode(code int) {
	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowStatusCounts[code]++
}

// recordBadStatus counts a request that received a non-2xx status, recording the status
// code and the error counters together under a single lock.
func (c *bandwidthChecker) recordBadStatus(code int) {
	c.counterRequestsErrored.Add(1)

	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowStatusCounts[code]++
	c.windowErrorCount++
}

// recordTransportError counts a call that never received a response at all: the request
// could not be built, the round trip failed (connect/DNS/TLS/timeout), or the body read failed
// mid-stream. The full error is logged at the call site; here it is bucketed by
// transportErrorKey so the emitted metric stays a handful of distinct causes rather than
// growing one entry per request.
func (c *bandwidthChecker) recordTransportError(err error) {
	c.counterRequestsErrored.Add(1)

	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowErrorCount++
	c.windowTransportErrors[transportErrorKey(err)]++
}

// formatCounts sorts a count map by key and renders it as "key (count), key (count), ...",
// the shape used for both the status-code and transport-error breakdowns below.
func formatCounts[K cmp.Ordered](counts map[K]int64) string {
	keys := make([]K, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%v (%d)", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

// transportErrorKey collapses a *net.OpError's connection-specific address (which differs
// on every request since keep-alives are disabled, so every retry would otherwise get its
// own map entry) down to its operation and underlying error, e.g. "read tcp: i/o timeout".
// Other error kinds (e.g. context.DeadlineExceeded) are already stable and are used as-is.
func transportErrorKey(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		key := opErr.Op
		if opErr.Net != "" {
			key += " " + opErr.Net
		}
		return key + ": " + opErr.Err.Error()
	}
	return err.Error()
}

// emitWindowMetric calculates the aggregated bandwidth for the current window,
// resets the window counters, and returns the metric. Called by Status endpoint.
func (c *bandwidthChecker) emitWindowMetric() *action_kit_api.Metric {
	c.windowMu.Lock()

	// Calculate window duration
	windowDuration := time.Since(c.windowStartTime)
	bytesDownloaded := c.windowBytesDownloaded
	requestCount := c.windowRequestCount
	errorCount := c.windowErrorCount
	statusCounts := c.windowStatusCounts
	transportErrors := c.windowTransportErrors

	c.resetWindowLocked()
	c.windowMu.Unlock()

	// Skip if no activity in this window (no bytes downloaded, no requests completed, no errors)
	if bytesDownloaded == 0 && requestCount == 0 && errorCount == 0 {
		log.Debug().Msg("No activity in measurement window, skipping metric")
		return nil
	}

	// Calculate bandwidth: total bytes in window / window duration
	windowSeconds := windowDuration.Seconds()
	if windowSeconds <= 0 {
		windowSeconds = 0.001
	}

	bandwidthBps := float64(bytesDownloaded*8) / windowSeconds
	bandwidthMbps := bandwidthBps / 1_000_000

	withinThreshold := c.isWithinThreshold(bandwidthBps, errorCount, bytesDownloaded)
	if withinThreshold {
		c.counterWindowSuccess.Add(1)
	} else {
		c.counterWindowFailed.Add(1)
	}

	metricLabels := windowMetricLabels(windowSnapshot{
		url:             c.state.URL.String(),
		bytesDownloaded: bytesDownloaded,
		duration:        windowDuration,
		requestCount:    requestCount,
		errorCount:      errorCount,
		withinThreshold: withinThreshold,
		bandwidthMbps:   bandwidthMbps,
		statusCounts:    statusCounts,
		transportErrors: transportErrors,
	})

	metric := &action_kit_api.Metric{
		Name:      new("bandwidth"),
		Metric:    metricLabels,
		Value:     math.Trunc(bandwidthMbps*100) / 100,
		Timestamp: time.Now(),
	}

	log.Debug().Msgf("Window metric: %.2f Mbps, Bytes: %d, Duration: %v, Requests: %d, Errors: %d, Within threshold: %v",
		bandwidthMbps, bytesDownloaded, windowDuration, requestCount, errorCount, withinThreshold)

	return metric
}

// isWithinThreshold reports whether a window's measured bandwidth and errors satisfy the
// configured thresholds. A window with errors but no data at all is treated as a genuine
// stall/outage regardless of the thresholds; a window with errors that still delivered data
// is judged on throughput alone, since long downloads span multiple windows and complete in
// bursts - such a window can legitimately have megabytes in flight with no request finishing.
func (c *bandwidthChecker) isWithinThreshold(bandwidthBps float64, errorCount, bytesDownloaded int64) bool {
	withinThreshold := true
	if c.state.MinBandwidthBps > 0 && bandwidthBps < float64(c.state.MinBandwidthBps) {
		withinThreshold = false
		log.Trace().Msgf("Window bandwidth %.2f bps is below minimum %d bps", bandwidthBps, c.state.MinBandwidthBps)
	}
	if c.state.MaxBandwidthBps > 0 && bandwidthBps > float64(c.state.MaxBandwidthBps) {
		withinThreshold = false
		log.Trace().Msgf("Window bandwidth %.2f bps is above maximum %d bps", bandwidthBps, c.state.MaxBandwidthBps)
	}
	if errorCount > 0 && bytesDownloaded == 0 {
		withinThreshold = false
	}
	return withinThreshold
}

// windowSnapshot holds one measurement window's aggregated results, passed to
// windowMetricLabels as a single value to keep that function's signature small.
type windowSnapshot struct {
	url             string
	bytesDownloaded int64
	duration        time.Duration
	requestCount    int64
	errorCount      int64
	withinThreshold bool
	bandwidthMbps   float64
	statusCounts    map[int]int64
	transportErrors map[string]int64
}

// windowMetricLabels builds one measurement window's metric labels, including the status-code
// and transport-error breakdowns when the window saw any.
func windowMetricLabels(w windowSnapshot) map[string]string {
	labels := map[string]string{
		"url":              w.url,
		"bytes_downloaded": strconv.FormatInt(w.bytesDownloaded, 10),
		"duration_ms":      strconv.FormatInt(w.duration.Milliseconds(), 10),
		"request_count":    strconv.FormatInt(w.requestCount, 10),
		"error_count":      strconv.FormatInt(w.errorCount, 10),
		"within_threshold": strconv.FormatBool(w.withinThreshold),
		"bandwidth":        strconv.FormatFloat(w.bandwidthMbps, 'g', -1, 64),
	}
	statusCounts := w.statusCounts
	transportErrors := w.transportErrors

	// Report the status code for every call that received a response, successful or not, as a
	// single aggregated field - the same "http_status" key the other HTTP checks report, since
	// a window can span calls that got different codes. expected_http_status mirrors the other
	// checks' per-request field: false if any call in the window got a non-2xx status, driving
	// the widget's "Unexpected Status" grouping the same way.
	if len(statusCounts) > 0 {
		allExpected := true
		for code := range statusCounts {
			if code < 200 || code >= 300 {
				allExpected = false
				break
			}
		}
		labels["http_status"] = formatCounts(statusCounts)
		labels["expected_http_status"] = strconv.FormatBool(allExpected)
	}

	// Differentiate transport-level failures (never received a response) by their actual error,
	// the same way the other HTTP checks do, and report them under the same "error" key the
	// other checks use so the widget's "Failure" grouping (keyed on "error" being set) applies
	// here too - a bare "request(s) failed" count isn't useful on its own.
	if len(transportErrors) > 0 {
		labels["error"] = formatCounts(transportErrors)
	}

	return labels
}

func loadBandwidthChecker(executionID uuid.UUID) (*bandwidthChecker, error) {
	checker, ok := bandwidthCheckers.Load(executionID)
	if !ok {
		return nil, fmt.Errorf("failed to load associated bandwidth checker")
	}
	return checker.(*bandwidthChecker), nil
}
