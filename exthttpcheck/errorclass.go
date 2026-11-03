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
	"slices"
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

// classifyError maps a transport error (one that produced no HTTP response) to the classes it matches. The
// first class is the primary one, reported as the "error_type" metric label. Every error matches at least one
// class, errorClassAny if no specific one applies. A DNS lookup cut off by the connect or read timeout matches
// both "timeout" (primary, the configured deadline was hit) and "dns".
func classifyError(err error) []string {
	if err == nil {
		return nil
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout || errors.Is(err, context.DeadlineExceeded) {
			return []string{errorClassTimeout, errorClassDNS}
		}
		return []string{errorClassDNS}
	}

	return []string{classifyNonDNSError(err)}
}

func classifyNonDNSError(err error) string {
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
	return anyInChain(err, func(e error) bool { return strings.HasPrefix(e.Error(), "tls: ") })
}

// anyInChain reports whether pred holds for err or any error it wraps, following both Unwrap() error and
// Unwrap() []error (errors.Join, fmt.Errorf with several %w).
func anyInChain(err error, pred func(error) bool) bool {
	if err == nil {
		return false
	}
	if pred(err) {
		return true
	}
	switch u := err.(type) {
	case interface{ Unwrap() error }:
		return anyInChain(u.Unwrap(), pred)
	case interface{ Unwrap() []error }:
		return slices.ContainsFunc(u.Unwrap(), func(e error) bool { return anyInChain(e, pred) })
	}
	return false
}

// errorWasExpected reports whether a transport error matching the given classes satisfies the expected status codes.
func errorWasExpected(expected []string, classes []string) bool {
	return slices.Contains(expected, errorClassAny) || slices.ContainsFunc(classes, func(c string) bool { return slices.Contains(expected, c) })
}
