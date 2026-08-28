// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package exthttpcheck

import (
	"net/http/httptrace"
	"time"
)

// Response-time measurement modes (config value of the "responseTimeMeasurement"
// parameter).
const (
	timeToFirstByte = "TIME_TO_FIRST_BYTE"
	timeToLastByte  = "TIME_TO_LAST_BYTE"
)

type requestTracer struct {
	httptrace.ClientTrace
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
func (t requestTracer) responseTime(measurement string) time.Duration {
	if measurement == timeToLastByte {
		return t.lastByteReceived.Sub(t.connectionStart)
	}
	return t.firstByteReceived.Sub(t.requestWritten)
}

func newRequestTracer() *requestTracer {
	t := &requestTracer{}

	t.ClientTrace = httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			// First connection attempt of the request wins; on a redirect chain
			// this keeps the last-byte measurement anchored to the very start.
			if t.connectionStart.IsZero() {
				t.connectionStart = time.Now()
			}
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			t.requestWritten = time.Now()
		},
		GotFirstResponseByte: func() {
			t.firstByteReceived = time.Now()
		},
	}

	return t
}
