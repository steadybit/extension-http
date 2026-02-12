// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type bandwidthChecker struct {
	// Window aggregation
	windowMu              sync.Mutex
	windowStartTime       time.Time
	windowBytesDownloaded int64
	windowRequestCount    int64
	windowErrorCount      int64

	// Counters for success rate calculation (per window)
	counterWindowSuccess atomic.Uint64
	counterWindowFailed  atomic.Uint64

	// Control
	stopped atomic.Bool
	state   *BandwidthCheckState
}

var bandwidthCheckers = sync.Map{}

func newBandwidthChecker(state *BandwidthCheckState) *bandwidthChecker {
	return &bandwidthChecker{
		state: state,
	}
}

func (c *bandwidthChecker) start() {
	// Initialize window
	c.windowMu.Lock()
	c.windowStartTime = time.Now()
	c.windowBytesDownloaded = 0
	c.windowRequestCount = 0
	c.windowErrorCount = 0
	c.windowMu.Unlock()

	// Start workers that continuously perform requests without delay
	for w := 1; w <= c.state.MaxConcurrent; w++ {
		go c.performBandwidthRequests()
	}

	log.Debug().Msgf("Started %d bandwidth workers", c.state.MaxConcurrent)
}

func (c *bandwidthChecker) stop() {
	c.stopped.Store(true)
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

	for !c.stopped.Load() {
		req, err := http.NewRequest("GET", c.state.URL.String(), nil)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create bandwidth request")
			c.recordError()
			continue
		}

		req.Header.Set("User-Agent", "steadybit/extension-http:"+extbuild.GetSemverVersionStringOrUnknown())
		for k, v := range c.state.Headers {
			req.Header.Add(k, v)
		}

		startTime := time.Now()
		response, err := client.Do(req)
		if err != nil {
			log.Error().Err(err).Msg("Failed to execute bandwidth request")
			c.recordError()
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			log.Error().Msgf("Unexpected HTTP status %d for bandwidth request to %s", response.StatusCode, c.state.URL.String())
			c.recordError()
			continue
		}

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
			log.Error().Err(readErr).Msg("Failed to read response body")
			c.recordError()
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
	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowRequestCount++
}

func (c *bandwidthChecker) recordError() {
	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	c.windowErrorCount++
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

	// Reset window
	c.windowStartTime = time.Now()
	c.windowBytesDownloaded = 0
	c.windowRequestCount = 0
	c.windowErrorCount = 0

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

	// Check thresholds
	withinThreshold := true
	if c.state.MinBandwidthBps > 0 && bandwidthBps < float64(c.state.MinBandwidthBps) {
		withinThreshold = false
		log.Trace().Msgf("Window bandwidth %.2f bps is below minimum %d bps", bandwidthBps, c.state.MinBandwidthBps)
	}
	if c.state.MaxBandwidthBps > 0 && bandwidthBps > float64(c.state.MaxBandwidthBps) {
		withinThreshold = false
		log.Trace().Msgf("Window bandwidth %.2f bps is above maximum %d bps", bandwidthBps, c.state.MaxBandwidthBps)
	}

	// Consider window failed if there were errors and no successful requests
	if errorCount > 0 && requestCount == 0 {
		withinThreshold = false
	}

	// Update success/failure counters
	if withinThreshold {
		c.counterWindowSuccess.Add(1)
	} else {
		c.counterWindowFailed.Add(1)
	}

	metric := &action_kit_api.Metric{
		Name: extutil.Ptr("bandwidth"),
		Metric: map[string]string{
			"url":              c.state.URL.String(),
			"bytes_downloaded": strconv.FormatInt(bytesDownloaded, 10),
			"duration_ms":      strconv.FormatInt(windowDuration.Milliseconds(), 10),
			"request_count":    strconv.FormatInt(requestCount, 10),
			"error_count":      strconv.FormatInt(errorCount, 10),
			"within_threshold": strconv.FormatBool(withinThreshold),
			"bandwidth":        strconv.FormatFloat(bandwidthMbps, 'g', -1, 64),
		},
		Value:     math.Trunc(bandwidthMbps*100) / 100,
		Timestamp: time.Now(),
	}

	log.Debug().Msgf("Window metric: %.2f Mbps, Bytes: %d, Duration: %v, Requests: %d, Errors: %d, Within threshold: %v",
		bandwidthMbps, bytesDownloaded, windowDuration, requestCount, errorCount, withinThreshold)

	return metric
}

func loadBandwidthChecker(executionID uuid.UUID) (*bandwidthChecker, error) {
	checker, ok := bandwidthCheckers.Load(executionID)
	if !ok {
		return nil, fmt.Errorf("failed to load associated bandwidth checker")
	}
	return checker.(*bandwidthChecker), nil
}
