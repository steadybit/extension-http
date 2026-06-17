// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package exthttpcheck

import (
	"net/http/httptrace"
	"sync"
	"time"
)

// Response-time measurement modes (config value of the "responseTimeMeasurement"
// parameter).
const (
	timeToFirstByte = "TIME_TO_FIRST_BYTE"
	timeToLastByte  = "TIME_TO_LAST_BYTE"
)

// requestTracer records request/response timing via httptrace callbacks. The
// callbacks are invoked from net/http's internal goroutines (GetConn and
// WroteRequest from the connection's writeLoop, GotFirstResponseByte from the
// readLoop), which can run concurrently with the worker goroutine that records
// the last byte and reads the timings after Do returns. The mutex guards the
// time fields against that data race.
type requestTracer struct {
	httptrace.ClientTrace
	mu                                                                   sync.Mutex
	connectionStart, requestWritten, firstByteReceived, lastByteReceived time.Time
}

// responseTime returns the measured duration for the given measurement mode:
//
//   - TIME_TO_LAST_BYTE: from the start of connection acquisition (DNS, TCP
//     connect and TLS handshake included) to the last response byte — the full
//     end-to-end time, so connection-level faults (injected latency, a slow
//     handshake, a delayed connect) and slow body downloads are both visible.
//   - anything else (default, first byte): from "request written" to the first
//     response byte — the server's processing time, excluding connection setup.
func (t *requestTracer) responseTime(measurement string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if measurement == timeToLastByte {
		return t.lastByteReceived.Sub(t.connectionStart)
	}
	return t.firstByteReceived.Sub(t.requestWritten)
}

// firstByteReceivedTime is the timestamp reported on the response-time metric.
func (t *requestTracer) firstByteReceivedTime() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.firstByteReceived
}

// markLastByteReceived records the end of the response body read. Called from
// the worker goroutine once io.ReadAll has returned.
func (t *requestTracer) markLastByteReceived() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastByteReceived = time.Now()
}

func newRequestTracer() *requestTracer {
	t := &requestTracer{}

	t.ClientTrace = httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			t.mu.Lock()
			defer t.mu.Unlock()
			// First connection attempt of the request wins; on a redirect chain
			// this keeps the last-byte measurement anchored to the very start.
			if t.connectionStart.IsZero() {
				t.connectionStart = time.Now()
			}
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.requestWritten = time.Now()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.firstByteReceived = time.Now()
		},
	}

	return t
}
