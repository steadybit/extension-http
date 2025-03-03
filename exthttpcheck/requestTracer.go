// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package exthttpcheck

import (
	"net/http/httptrace"
	"time"
)

type requestTracer struct {
	httptrace.ClientTrace
	requestWritten, firstByteReceived time.Time
}

func (t requestTracer) responseTime() time.Duration {
	return t.firstByteReceived.Sub(t.requestWritten)
}

func newRequestTracer() *requestTracer {
	t := &requestTracer{}

	// see seems to break a configured proxy. maybe we can use it in the future and configure the proxy here
	t.ClientTrace = httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			t.requestWritten = time.Now()
		},
		GotFirstResponseByte: func() {
			t.firstByteReceived = time.Now()
		},
	}

	return t
}
