// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package exthttpcheck

import (
	"net/http/httptrace"
	"time"
)

type requestTracer struct {
	httptrace.ClientTrace
	connectionStart, firstByteReceived time.Time
}

// responseTime is the time from the start of connection acquisition — DNS, TCP
// connect and the TLS handshake included — to the first response byte.
//
// Measuring from connection start rather than from "request written" is what
// makes connection-level faults visible: a transparent proxy injecting latency,
// a slow TLS handshake, or a delayed connect all land before the request bytes
// are written, so a WroteRequest→GotFirstResponseByte window would shift as a
// whole and report no change. Including the setup phases surfaces them.
func (t requestTracer) responseTime() time.Duration {
	return t.firstByteReceived.Sub(t.connectionStart)
}

func newRequestTracer() *requestTracer {
	t := &requestTracer{}

	t.ClientTrace = httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			// First connection attempt of the request wins; on a redirect chain
			// this keeps the measurement anchored to the very start.
			if t.connectionStart.IsZero() {
				t.connectionStart = time.Now()
			}
		},
		GotFirstResponseByte: func() {
			t.firstByteReceived = time.Now()
		},
	}

	return t
}
