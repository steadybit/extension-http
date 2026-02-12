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
	"math/big"
	"net"
	"net/http"
	"os"
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
	defer server.Close()

	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-http",
		Port: 8085,
		ExtraArgs: func(m *e2e.Minikube) []string {
			return []string{
				//"--set", "logging.level=debug",
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
			Test: testPeriodically,
		},
		{
			Name: "fixAmount",
			Test: testFixAmount,
		},
	})
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
		w.Write([]byte("Hello from HTTPS server with self-signed certificate!"))
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
		close(readyCh) // Signal that the server goroutine has started
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Failed to start HTTPS server")
		}
		wg.Done()
	}()

	// Wait for server to be ready with retry and backoff
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
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
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

func testPeriodically(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	log.Info().Msg("Starting testPeriodically")
	netperf := e2e.Netperf{Minikube: m}
	err := netperf.Deploy("delay")
	defer func() { _ = netperf.Delete() }()
	require.NoError(t, err)

	tests := []struct {
		name               string
		url                string
		timeout            float64
		insecureSkipVerify bool
		WantedFailure      bool
	}{
		{
			name:          "should check status ok",
			url:           "https://hub-dev.steadybit.com",
			timeout:       3000,
			WantedFailure: false,
		},
		{
			name:          "should check status not ok",
			url:           "https://hub-dev.steadybit.com",
			timeout:       1,
			WantedFailure: true,
		}, {
			name:               "should check status for bad ssl website",
			url:                "https://self-signed.badssl.com/",
			timeout:            30000,
			insecureSkipVerify: false,
			WantedFailure:      true,
		}, {
			name:               "should check status for bad ssl website with insecureSkipVerify",
			url:                "https://self-signed.badssl.com/",
			timeout:            30000,
			insecureSkipVerify: true,
			WantedFailure:      false,
		}, {
			name:               "should check status with self-signed certificate",
			url:                "https://host.minikube.internal:8443",
			timeout:            30000,
			insecureSkipVerify: false,
			WantedFailure:      false,
		},
	}

	require.NoError(t, err)

	for _, tt := range tests {

		config := struct {
			Duration           int           `json:"duration"`
			Url                string        `json:"url"`
			ConnectTimeout     float64       `json:"connectTimeout"`
			RequestsPerSecond  float64       `json:"requestsPerSecond"`
			Method             string        `json:"method"`
			MaxConcurrent      float64       `json:"maxConcurrent"`
			StatusCode         string        `json:"statusCode"`
			ReadTimeout        float64       `json:"readTimeout"`
			Headers            []interface{} `json:"headers"`
			InsecureSkipVerify bool          `json:"insecureSkipVerify"`
		}{
			Duration:           10000,
			Url:                tt.url,
			ConnectTimeout:     tt.timeout,
			RequestsPerSecond:  2,
			Method:             "GET",
			MaxConcurrent:      1,
			StatusCode:         "200",
			ReadTimeout:        tt.timeout,
			Headers:            []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
			InsecureSkipVerify: tt.insecureSkipVerify,
		}

		t.Run(tt.name, func(t *testing.T) {
			action, err := e.RunAction(exthttpcheck.ActionIDPeriodically, nil, config, nil)
			defer func() { _ = action.Cancel() }()
			require.NoError(t, err)

			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				metrics := action.Metrics()
				for _, metric := range metrics {
					if !tt.WantedFailure {
						if metric.Metric["error"] != "" {
							log.Info().Msgf("Metric error: %v", metric.Metric["error"])
						}
						assert.Equal(c, "200", metric.Metric["http_status"])
					} else {
						assert.NotEqual(c, "200", metric.Metric["http_status"])
						//error -> Get "https://hub-dev.steadybit.com": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
						assert.Contains(c, metric.Metric["error"], "context deadline exceeded")
					}
				}
			}, 5*time.Second, 500*time.Millisecond)

			require.NoError(t, action.Cancel())
		})
	}
}

func testFixAmount(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	log.Info().Msg("Starting testPeriodically")
	netperf := e2e.Netperf{Minikube: m}
	err := netperf.Deploy("delay")
	defer func() { _ = netperf.Delete() }()
	require.NoError(t, err)

	tests := []struct {
		name               string
		url                string
		timeout            float64
		WantedFailure      bool
		insecureSkipVerify bool
	}{
		{
			name:          "should check status ok",
			url:           "https://hub-dev.steadybit.com",
			timeout:       3000,
			WantedFailure: false,
		},
		{
			name:          "should check status not ok",
			url:           "https://hub-dev.steadybit.com",
			timeout:       1,
			WantedFailure: true,
		},
		{
			name:               "should check status for bad ssl website",
			url:                "https://self-signed.badssl.com/",
			timeout:            30000,
			insecureSkipVerify: false,
			WantedFailure:      true,
		}, {
			name:               "should check status for bad ssl website with insecureSkipVerify",
			url:                "https://self-signed.badssl.com/",
			timeout:            30000,
			insecureSkipVerify: true,
			WantedFailure:      false,
		},
		{
			name:               "should check status with self-signed certificate",
			url:                "https://host.minikube.internal:8443",
			timeout:            30000,
			insecureSkipVerify: false,
			WantedFailure:      false,
		},
	}

	require.NoError(t, err)

	for _, tt := range tests {

		config := struct {
			Duration           int           `json:"duration"`
			Url                string        `json:"url"`
			ConnectTimeout     float64       `json:"connectTimeout"`
			NumberOfRequests   float64       `json:"numberOfRequests"`
			Method             string        `json:"method"`
			MaxConcurrent      float64       `json:"maxConcurrent"`
			StatusCode         string        `json:"statusCode"`
			ReadTimeout        float64       `json:"readTimeout"`
			Headers            []interface{} `json:"headers"`
			InsecureSkipVerify bool          `json:"insecureSkipVerify"`
		}{
			Duration:           10000,
			Url:                tt.url,
			ConnectTimeout:     tt.timeout,
			NumberOfRequests:   20,
			Method:             "GET",
			MaxConcurrent:      1,
			StatusCode:         "200",
			ReadTimeout:        tt.timeout,
			Headers:            []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
			InsecureSkipVerify: tt.insecureSkipVerify,
		}

		t.Run(tt.name, func(t *testing.T) {
			action, err := e.RunAction(exthttpcheck.ActionIDFixedAmount, nil, config, nil)
			defer func() { _ = action.Cancel() }()
			require.NoError(t, err)

			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				metrics := action.Metrics()
				for _, metric := range metrics {
					if !tt.WantedFailure {
						assert.Equal(c, metric.Metric["http_status"], "200")
					} else {
						assert.NotEqual(c, metric.Metric["http_status"], "200")
						//error -> Get "https://hub-dev.steadybit.com": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
						assert.Contains(c, metric.Metric["error"], "context deadline exceeded")
					}
				}
			}, 5*time.Second, 500*time.Millisecond)

			require.NoError(t, action.Cancel())
		})
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

	// Create certificate template
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
		DNSNames:              []string{"localhost"},
	}

	// Create certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	// Create temporary certificate file
	certFile, err := os.CreateTemp("", "cert*.pem")
	if err != nil {
		return nil, err
	}
	defer certFile.Close()

	// Write certificate to file
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		return nil, err
	}

	// Create temporary key file
	keyFile, err := os.CreateTemp("", "key*.pem")
	if err != nil {
		return nil, err
	}
	defer keyFile.Close()

	// Write private key to file
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	err = pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})
	if err != nil {
		return nil, err
	}

	// publish file path via environment variables
	err = os.Setenv("CERT_FILE", certFile.Name())
	if err != nil {
		return nil, err
	}
	err = os.Setenv("KEY_FILE", keyFile.Name())
	if err != nil {
		return nil, err
	}

	cleanup := func() {
		// delete the temporary files
		if err := os.Remove(certFile.Name()); err != nil {
			log.Error().Err(err).Msgf("Failed to remove temporary certificate file: %s", certFile.Name())
		}
		if err := os.Remove(keyFile.Name()); err != nil {
			log.Error().Err(err).Msgf("Failed to remove temporary key file: %s", keyFile.Name())
		}
		// Unset environment variables
		if err := os.Unsetenv("CERT_FILE"); err != nil {
			log.Error().Err(err).Msg("Failed to unset CERT_FILE environment variable")
		}
		if err := os.Unsetenv("KEY_FILE"); err != nil {
			log.Error().Err(err).Msg("Failed to unset KEY_FILE environment variable")
		}
	}
	return cleanup, nil
}
