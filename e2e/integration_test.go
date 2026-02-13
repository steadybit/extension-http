// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Steadybit GmbH

package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/extension-http/exthttpcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithMinikube(t *testing.T) {
	cleanup, err := generateSelfSignedCert()
	require.NoError(t, err)
	defer cleanup()

	server := startLocalServerWithSelfSignedCertificate(t)
	defer closeSilent(server)

	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-http",
		Port: 8085,
		ExtraArgs: func(m *e2e.Minikube) []string {
			return []string{
				"--set", "logging.level=debug",
				"--set", "extraVolumes[0].name=extra-certs",
				"--set", "extraVolumes[0].configMap.name=self-signed-cert",
				"--set", "extraVolumeMounts[0].name=extra-certs",
				"--set", "extraVolumeMounts[0].mountPath=/etc/ssl/extra-certs",
				"--set", "extraVolumeMounts[0].readOnly=true",
				"--set", "extraEnv[0].name=SSL_CERT_DIR",
				"--set", "extraEnv[0].value=/etc/ssl/extra-certs:/etc/ssl/certs",
			}
		},
	}

	e2e.WithMinikube(t, e2e.DefaultMinikubeOpts().AfterStart(installConfigMap), &extFactory, []e2e.WithMinikubeTestCase{
		{
			Name: "periodically",
			Test: runHTTPCheckTests(exthttpcheck.ActionIDPeriodically, func(tt testcase) map[string]any {
				config := httpCheckConfig(tt)
				config["requestsPerSecond"] = 2.0
				return config
			}),
		},
		{
			Name: "fixAmount",
			Test: runHTTPCheckTests(exthttpcheck.ActionIDFixedAmount, func(tt testcase) map[string]any {
				config := httpCheckConfig(tt)
				config["numberOfRequests"] = 20.0
				return config
			}),
		},
	})
}

type testcase struct {
	name               string
	url                string
	timeout            float64
	insecureSkipVerify bool
	wantedFailure      string
	wantedSuccessCount int
	wantedFailureCount int
}

var httpCheckTests = []testcase{
	{
		name:               "should check status ok",
		url:                "https://steadybit.com",
		timeout:            5000,
		wantedFailure:      "",
		wantedSuccessCount: 20,
		wantedFailureCount: 0,
	},
	{
		name:               "should check status timed out",
		url:                "https://steadybit.com",
		timeout:            1,
		wantedFailure:      "<timeout>",
		wantedSuccessCount: 0,
		wantedFailureCount: 20,
	},
	{
		name:               "should check status for bad ssl website",
		url:                "https://self-signed.badssl.com/",
		timeout:            30000,
		insecureSkipVerify: false,
		wantedFailure:      "failed to verify certificate",
		wantedSuccessCount: 0,
		wantedFailureCount: 20,
	},
	{
		name:               "should check status for bad ssl website with insecureSkipVerify",
		url:                "https://self-signed.badssl.com/",
		timeout:            30000,
		insecureSkipVerify: true,
		wantedFailure:      "",
		wantedSuccessCount: 20,
		wantedFailureCount: 0,
	},
	{
		name:               "should check status with self-signed certificate",
		url:                "https://host.minikube.internal:8443",
		timeout:            30000,
		insecureSkipVerify: false,
		wantedFailure:      "",
		wantedSuccessCount: 20,
		wantedFailureCount: 0,
	},
}

func httpCheckConfig(tt testcase) map[string]any {
	return map[string]any{
		"duration":           10000,
		"url":                tt.url,
		"connectTimeout":     tt.timeout,
		"method":             "GET",
		"maxConcurrent":      1.0,
		"statusCode":         "200",
		"readTimeout":        tt.timeout,
		"headers":            []any{map[string]any{"key": "test", "value": "test"}},
		"insecureSkipVerify": tt.insecureSkipVerify,
	}
}

func runHTTPCheckTests(actionID string, buildConfig func(tt testcase) map[string]any) func(*testing.T, *e2e.Minikube, *e2e.Extension) {
	return func(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
		for _, tt := range httpCheckTests {
			t.Run(tt.name, func(t *testing.T) {
				config := buildConfig(tt)

				action, err := e.RunAction(actionID, nil, config, nil)
				defer func() { _ = action.Cancel() }()
				require.NoError(t, err)

				require.NoError(t, action.Wait())

				metrics := action.Metrics()
				assert.NotEmpty(t, metrics)

				var failures, successes int
				for _, metric := range metrics {
					if _, ok := metric.Metric["error"]; ok {
						failures++
					} else {
						successes++
					}

					if tt.wantedFailure == "" {
						assert.Empty(t, metric.Metric["error"], "expected no error")
						assert.Equal(t, "200", metric.Metric["http_status"])
					} else if tt.wantedFailure == "<timeout>" {
						assert.True(t, strings.Contains(metric.Metric["error"], "i/o timeout") || strings.Contains(metric.Metric["error"], "context deadline exceeded"))
					} else {
						assert.Contains(t, metric.Metric["error"], tt.wantedFailure)
					}
				}

				assert.InDelta(t, tt.wantedSuccessCount, successes, 1, "unexpected number of successful requests")
				assert.InDelta(t, tt.wantedFailureCount, failures, 1, "unexpected number of failed requests")

				configDuration := time.Duration(config["duration"].(int)) * time.Millisecond
				assert.InDelta(t, configDuration, action.Duration(), 2*float64(time.Second), "action duration should be close to configured duration")
			})
		}
	}
}

// generateSelfSignedCert creates a self-signed certificate and returns the paths
// to the certificate and private key files
func generateSelfSignedCert() (func(), error) {
	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "localhost",
			Organization: []string{"Steadybit Test"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost", "host.minikube.internal"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	certFile, err := os.CreateTemp("", "cert*.pem")
	if err != nil {
		return nil, err
	}
	defer closeSilent(certFile)

	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		return nil, err
	}

	keyFile, err := os.CreateTemp("", "key*.pem")
	if err != nil {
		return nil, err
	}
	defer closeSilent(keyFile)

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	err = pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})
	if err != nil {
		return nil, err
	}

	err = os.Setenv("CERT_FILE", certFile.Name())
	if err != nil {
		return nil, err
	}
	err = os.Setenv("KEY_FILE", keyFile.Name())
	if err != nil {
		return nil, err
	}

	cleanup := func() {
		if err := os.Remove(certFile.Name()); err != nil {
			log.Error().Err(err).Msgf("Failed to remove temporary certificate file: %s", certFile.Name())
		}
		if err := os.Remove(keyFile.Name()); err != nil {
			log.Error().Err(err).Msgf("Failed to remove temporary key file: %s", keyFile.Name())
		}
		if err := os.Unsetenv("CERT_FILE"); err != nil {
			log.Error().Err(err).Msg("Failed to unset CERT_FILE environment variable")
		}
		if err := os.Unsetenv("KEY_FILE"); err != nil {
			log.Error().Err(err).Msg("Failed to unset KEY_FILE environment variable")
		}
	}
	return cleanup, nil
}

func startLocalServerWithSelfSignedCertificate(t *testing.T) *http.Server {
	serverCert, err := tls.LoadX509KeyPair(os.Getenv("CERT_FILE"), os.Getenv("KEY_FILE"))
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello from HTTPS server with self-signed certificate!"))
	})

	server := &http.Server{
		Addr:      ":8443",
		TLSConfig: tlsConfig,
		Handler:   mux,
	}

	readyCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		log.Info().Msg("Starting HTTPS server on port 8443")
		close(readyCh)
		if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Failed to start HTTPS server")
		}
		wg.Done()
	}()

	waitForServerReady(t, "https://localhost:8443", readyCh)

	return server
}

func waitForServerReady(t *testing.T, url string, readyCh chan struct{}) {
	timeout := time.After(10 * time.Second)
	backoff := 100 * time.Millisecond
	<-readyCh // Wait for server goroutine to start
	for {
		select {
		case <-timeout:
			t.Fatalf("Server did not become ready in time")
			return
		default:
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			resp, err := client.Get(url)
			if err == nil && resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				return
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
			time.Sleep(backoff)
			if backoff < 2*time.Second {
				backoff *= 2
			}
		}
	}
}

func installConfigMap(m *e2e.Minikube) error {
	err := m.CreateConfigMap("default", "self-signed-cert", os.Getenv("CERT_FILE"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create ConfigMap with self-signed certificate")
		return err
	}
	return nil
}

type closable interface {
	Close() error
}

func closeSilent(c closable) {
	_ = c.Close()
}
