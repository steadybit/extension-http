// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The two measurement modes read different windows: first byte excludes
// connection setup, last byte spans connection start → last response byte.
func Test_requestTracer_measurements(t *testing.T) {
	base := time.Now()
	tr := requestTracer{
		connectionStart:   base,
		requestWritten:    base.Add(100 * time.Millisecond), // 100ms of DNS/connect/TLS
		firstByteReceived: base.Add(150 * time.Millisecond),
		lastByteReceived:  base.Add(300 * time.Millisecond),
	}

	// First byte = firstByteReceived - requestWritten = 50ms (setup excluded).
	require.Equal(t, 50*time.Millisecond, tr.responseTime(timeToFirstByte))
	require.Equal(t, 50*time.Millisecond, tr.responseTime(""), "default is first byte")

	// Last byte = lastByteReceived - connectionStart = 300ms (setup + body included).
	require.Equal(t, 300*time.Millisecond, tr.responseTime(timeToLastByte))
}

// A connection-setup delay must land in the last-byte measurement but not the
// first-byte one — this is the case the "time to last byte" option surfaces.
func Test_requestTracer_lastByte_capturesConnectionDelay(t *testing.T) {
	base := time.Now()
	const setup = 200 * time.Millisecond
	tr := requestTracer{
		connectionStart:   base,
		requestWritten:    base.Add(setup),
		firstByteReceived: base.Add(setup + 10*time.Millisecond),
		lastByteReceived:  base.Add(setup + 30*time.Millisecond),
	}

	require.Less(t, tr.responseTime(timeToFirstByte), setup, "first byte excludes the connection-setup delay")
	require.GreaterOrEqual(t, tr.responseTime(timeToLastByte), setup, "last byte includes the connection-setup delay")
}
