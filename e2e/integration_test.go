// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Steadybit GmbH

package e2e

import (
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/extension-http/exthttpcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestWithMinikube(t *testing.T) {
	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-http",
		Port: 8085,
		ExtraArgs: func(m *e2e.Minikube) []string {
			return []string{
				//"--set", "logging.level=debug",
			}
		},
	}

	e2e.WithDefaultMinikube(t, &extFactory, []e2e.WithMinikubeTestCase{
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

func testPeriodically(t *testing.T, m *e2e.Minikube, e *e2e.Extension) {
	log.Info().Msg("Starting testPeriodically")
	netperf := e2e.Netperf{Minikube: m}
	err := netperf.Deploy("delay")
	defer func() { _ = netperf.Delete() }()
	require.NoError(t, err)

	tests := []struct {
		name          string
		url           string
		timeout       float64
		WantedFailure bool
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
	}

	require.NoError(t, err)

	for _, tt := range tests {

		config := struct {
			Duration          int           `json:"duration"`
			Url               string        `json:"url"`
			ConnectTimeout    float64       `json:"connectTimeout"`
			RequestsPerSecond float64       `json:"requestsPerSecond"`
			Method            string        `json:"method"`
			MaxConcurrent     float64       `json:"maxConcurrent"`
			StatusCode        string        `json:"statusCode"`
			ReadTimeout       float64       `json:"readTimeout"`
			Headers           []interface{} `json:"headers"`
		}{
			Duration:          10000,
			Url:               tt.url,
			ConnectTimeout:    tt.timeout,
			RequestsPerSecond: 2,
			Method:            "GET",
			MaxConcurrent:     1,
			StatusCode:        "200",
			ReadTimeout:       tt.timeout,
			Headers:           []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		}

		t.Run(tt.name, func(t *testing.T) {
			action, err := e.RunAction(exthttpcheck.TargetIDPeriodically, nil, config, nil)
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
		name          string
		url           string
		timeout       float64
		WantedFailure bool
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
	}

	require.NoError(t, err)

	for _, tt := range tests {

		config := struct {
			Duration         int           `json:"duration"`
			Url              string        `json:"url"`
			ConnectTimeout   float64       `json:"connectTimeout"`
			NumberOfRequests float64       `json:"numberOfRequests"`
			Method           string        `json:"method"`
			MaxConcurrent    float64       `json:"maxConcurrent"`
			StatusCode       string        `json:"statusCode"`
			ReadTimeout      float64       `json:"readTimeout"`
			Headers          []interface{} `json:"headers"`
		}{
			Duration:         10000,
			Url:              tt.url,
			ConnectTimeout:   tt.timeout,
			NumberOfRequests: 20,
			Method:           "GET",
			MaxConcurrent:    1,
			StatusCode:       "200",
			ReadTimeout:      tt.timeout,
			Headers:          []interface{}{map[string]interface{}{"key": "test", "value": "test"}},
		}

		t.Run(tt.name, func(t *testing.T) {
			action, err := e.RunAction(exthttpcheck.TargetIDFixedAmount, nil, config, nil)
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
