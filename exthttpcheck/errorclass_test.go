// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_classifyError(t *testing.T) {
	opErr := func(op string, err error) error {
		return &url.Error{Op: "Get", URL: "http://example.com", Err: &net.OpError{Op: op, Net: "tcp", Err: &os.SyscallError{Syscall: op, Err: err}}}
	}
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"refused", opErr("dial", syscall.ECONNREFUSED), []string{errorClassRefused}},
		{"reset", opErr("read", syscall.ECONNRESET), []string{errorClassReset}},
		{"broken pipe", opErr("write", syscall.EPIPE), []string{errorClassReset}},
		{"eof before response", &url.Error{Op: "Get", URL: "http://example.com", Err: io.EOF}, []string{errorClassReset}},
		{"host unreachable", opErr("dial", syscall.EHOSTUNREACH), []string{errorClassUnreachable}},
		{"network unreachable", opErr("dial", syscall.ENETUNREACH), []string{errorClassUnreachable}},
		{"context deadline", &url.Error{Op: "Get", URL: "http://example.com", Err: context.DeadlineExceeded}, []string{errorClassTimeout}},
		{"dial i/o timeout", &url.Error{Op: "Get", URL: "http://example.com", Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}}, []string{errorClassTimeout}},
		{"dns not found", &url.Error{Op: "Get", URL: "http://example.com", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true}}}, []string{errorClassDNS}},
		{"dns timeout is timeout and dns", &url.Error{Op: "Get", URL: "http://example.com", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "i/o timeout", IsTimeout: true}}}, []string{errorClassTimeout, errorClassDNS}},
		{"joined tls error", &url.Error{Op: "Get", URL: "https://example.com", Err: errors.Join(errors.New("close failed"), errors.New("tls: handshake failure"))}, []string{errorClassTLS}},
		{"tls unknown authority", &url.Error{Op: "Get", URL: "https://example.com", Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}, []string{errorClassTLS}},
		{"tls hostname", &url.Error{Op: "Get", URL: "https://example.com", Err: x509.HostnameError{Certificate: &x509.Certificate{}, Host: "example.com"}}, []string{errorClassTLS}},
		{"tls record header", &url.Error{Op: "Get", URL: "https://example.com", Err: tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}}, []string{errorClassTLS}},
		{"tls alert", &url.Error{Op: "Get", URL: "https://example.com", Err: fmt.Errorf("remote error: %w", tls.AlertError(40))}, []string{errorClassTLS}},
		{"tls plain handshake error", &url.Error{Op: "Get", URL: "https://example.com", Err: errors.New("tls: handshake failure")}, []string{errorClassTLS}},
		{"redirect loop", &url.Error{Op: "Get", URL: "http://example.com", Err: errors.New("stopped after 10 redirects")}, []string{errorClassAny}},
		{"malformed response", &url.Error{Op: "Get", URL: "http://example.com", Err: errors.New("net/http: HTTP/1.x transport connection broken: malformed HTTP response")}, []string{errorClassAny}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyError(tt.err))
		})
	}
}

func Test_errorWasExpected(t *testing.T) {
	assert.True(t, errorWasExpected([]string{"200", "reset"}, []string{errorClassReset}))
	assert.False(t, errorWasExpected([]string{"200", "reset"}, []string{errorClassTimeout}))
	assert.True(t, errorWasExpected([]string{"error"}, []string{errorClassTimeout}))
	assert.True(t, errorWasExpected([]string{"error"}, []string{errorClassAny}))
	assert.False(t, errorWasExpected([]string{"timeout"}, []string{errorClassAny}))
	assert.False(t, errorWasExpected([]string{"200-299"}, []string{errorClassRefused}))
	// a DNS lookup cut off by the deadline satisfies both tokens
	assert.True(t, errorWasExpected([]string{"timeout"}, []string{errorClassTimeout, errorClassDNS}))
	assert.True(t, errorWasExpected([]string{"dns"}, []string{errorClassTimeout, errorClassDNS}))
}

// The following tests produce real transport errors through the http client used by the check, so the
// classification is verified against what net/http actually returns, not only against synthetic errors.

func runSingleRequest(t *testing.T, target string, expected []string, readTimeout time.Duration) *httpChecker {
	t.Helper()
	u, err := url.Parse(target)
	require.NoError(t, err)
	state := &HTTPCheckState{
		MaxConcurrent:        1,
		NumberOfRequests:     1,
		DelayBetweenRequests: time.Second,
		ExpectedStatusCodes:  expected,
		URL:                  *u,
		Method:               "GET",
		ReadTimeout:          readTimeout,
		ConnectionTimeout:    readTimeout,
	}
	checker := newHttpChecker(context.Background(), state)
	checker.start()
	require.Eventually(t, func() bool {
		return checker.counters.success.Load()+checker.counters.failed.Load() >= 1
	}, 10*time.Second, 10*time.Millisecond)
	checker.shutdown()
	return checker
}

func singleErrorType(t *testing.T, c *httpChecker) string {
	t.Helper()
	metrics := c.getLatestMetrics()
	require.Len(t, metrics, 1)
	require.NotEmpty(t, metrics[0].Metric["error"])
	return metrics[0].Metric["error_type"]
}

func closedPortURL(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return "http://" + addr
}

func resettingServer(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0) // send RST instead of FIN
		}
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestHttpChecker_TransportErrorClasses(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer untrusted.Close()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"refused", closedPortURL(t), errorClassRefused},
		{"reset", resettingServer(t), errorClassReset},
		{"timeout", slow.URL, errorClassTimeout},
		{"tls", untrusted.URL, errorClassTLS},
		{"dns", "http://does-not-exist.invalid", errorClassDNS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedMatch := runSingleRequest(t, tt.url, []string{"200", tt.want}, 500*time.Millisecond)
			assert.Equal(t, tt.want, singleErrorType(t, expectedMatch))
			assert.Equal(t, uint64(1), expectedMatch.counters.success.Load(), "%s should be accepted when %q is expected", tt.name, tt.want)

			catchAll := runSingleRequest(t, tt.url, []string{"error"}, 500*time.Millisecond)
			assert.Equal(t, uint64(1), catchAll.counters.success.Load(), "%s should be accepted by the 'error' token", tt.name)

			notExpected := runSingleRequest(t, tt.url, []string{"200-299"}, 500*time.Millisecond)
			assert.Equal(t, uint64(1), notExpected.counters.failed.Load(), "%s should fail when only HTTP statuses are expected", tt.name)
		})
	}
}
