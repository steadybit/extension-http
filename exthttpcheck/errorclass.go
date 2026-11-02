// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
)

// Tokens usable in the status code expression to expect a request that yields no HTTP status.
const (
	errorClassRefused     = "refused"
	errorClassTimeout     = "timeout"
	errorClassReset       = "reset"
	errorClassDNS         = "dns"
	errorClassTLS         = "tls"
	errorClassUnreachable = "unreachable"
	// errorClassAny matches every transport error, including the ones not covered by a specific class
	// (e.g. protocol errors, redirect loops).
	errorClassAny = "error"
)

var errorClassTokens = []string{
	errorClassRefused,
	errorClassTimeout,
	errorClassReset,
	errorClassDNS,
	errorClassTLS,
	errorClassUnreachable,
	errorClassAny,
}

// classifyError maps a transport error (one that produced no HTTP response) to one of the specific error
// classes, or to errorClassAny if none applies. The checks are ordered so that an error is assigned exactly
// one class: a DNS lookup that timed out is reported as "dns", not "timeout".
func classifyError(err error) string {
	if err == nil {
		return ""
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return errorClassDNS
	}

	if isTLSError(err) {
		return errorClassTLS
	}

	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return errorClassRefused
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTDOWN):
		return errorClassUnreachable
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ECONNABORTED), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errorClassReset
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return errorClassTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errorClassTimeout
	}

	return errorClassAny
}

func isTLSError(err error) bool {
	var (
		recordHeaderErr     tls.RecordHeaderError
		alertErr            tls.AlertError
		certVerificationErr *tls.CertificateVerificationError
		unknownAuthorityErr x509.UnknownAuthorityError
		hostnameErr         x509.HostnameError
		certInvalidErr      x509.CertificateInvalidError
	)
	if errors.As(err, &recordHeaderErr) || errors.As(err, &alertErr) || errors.As(err, &certVerificationErr) ||
		errors.As(err, &unknownAuthorityErr) || errors.As(err, &hostnameErr) || errors.As(err, &certInvalidErr) {
		return true
	}
	// Most other handshake failures in crypto/tls are plain errors prefixed with "tls: ".
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.HasPrefix(e.Error(), "tls: ") {
			return true
		}
	}
	return false
}

// errorWasExpected reports whether a transport error of the given class satisfies the expected status codes.
func errorWasExpected(expected []string, class string) bool {
	for _, e := range expected {
		if e == errorClassAny || e == class {
			return true
		}
	}
	return false
}
