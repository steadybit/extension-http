// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package exthttpcheck

import (
	"net/http/httptrace"
	"sync"
	"time"
)

// requestTracer records request/response timing via httptrace callbacks. The
// callbacks are invoked from net/http's internal goroutines (WroteRequest from
// the connection's writeLoop, GotFirstResponseByte from the readLoop), which can
// run concurrently with the worker goroutine reading the timings after Do
// returns. The mutex guards the time fields against that data race.
type requestTracer struct {
	httptrace.ClientTrace
	mu                                sync.Mutex
	requestWritten, firstByteReceived time.Time
}

func (t *requestTracer) responseTime() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.firstByteReceived.Sub(t.requestWritten)
}

func (t *requestTracer) firstByteReceivedTime() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.firstByteReceived
}

func newRequestTracer() *requestTracer {
	t := &requestTracer{}

	t.ClientTrace = httptrace.ClientTrace{
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
