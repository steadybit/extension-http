// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-http/config"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

const (
	ActionIDBandwidth   = "com.steadybit.extension_http.check.bandwidth"
	actionIconBandwidth = "data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20width%3D%2224%22%20height%3D%2224%22%20viewBox%3D%220%200%2024%2024%22%20fill%3D%22none%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%221.6%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%3E%3Cpath%20d%3D%22M22%2012h-4l-3%209L9%203l-3%209H2%22%2F%3E%3C%2Fsvg%3E"
)

// bandwidthSuccessRate reuses the shared success-rate parameter but describes it
// in terms of measurement windows, which is how the bandwidth check evaluates it.
var bandwidthSuccessRate = func() action_kit_api.ActionParameter {
	p := successRate
	p.Description = new("Percentage of measurement windows that must be within the bandwidth thresholds. Evaluated at the end of the duration.")
	return p
}()

type httpCheckActionBandwidth struct{}

type BandwidthCheckState struct {
	ExecutionID        uuid.UUID
	URL                url.URL
	Headers            map[string]string
	MinBandwidthBps    int64 // minimum bandwidth in bits per second
	MaxBandwidthBps    int64 // maximum bandwidth in bits per second
	SuccessRate        int   // required success rate percentage
	ConnectionTimeout  time.Duration
	ReadTimeout        time.Duration
	FollowRedirects    bool
	InsecureSkipVerify bool
	MaxConcurrent      int
	FailEarly          bool
	// ExpectedWindows is the number of measurement windows expected over the whole step (~1 per
	// second). When FailEarly is enabled it is used to determine whether the required success rate
	// can still be reached.
	ExpectedWindows uint64
}

var (
	_ action_kit_sdk.Action[BandwidthCheckState]           = (*httpCheckActionBandwidth)(nil)
	_ action_kit_sdk.ActionWithStatus[BandwidthCheckState] = (*httpCheckActionBandwidth)(nil)
	_ action_kit_sdk.ActionWithStop[BandwidthCheckState]   = (*httpCheckActionBandwidth)(nil)
)

func NewHTTPCheckActionBandwidth() action_kit_sdk.Action[BandwidthCheckState] {
	return &httpCheckActionBandwidth{}
}

func (a *httpCheckActionBandwidth) NewEmptyState() BandwidthCheckState {
	return BandwidthCheckState{}
}

func (a *httpCheckActionBandwidth) Describe() action_kit_api.ActionDescription {
	var widgetToUse *[]action_kit_api.Widget
	if config.Config.EnableWidgetBackwardCompatibility {
		widgetToUse = new([]action_kit_api.Widget{
			action_kit_api.PredefinedWidget{
				Type:               action_kit_api.ComSteadybitWidgetPredefined,
				PredefinedWidgetId: "com.steadybit.widget.predefined.HttpCheck",
			},
		})
	} else {
		widgetToUse = new([]action_kit_api.Widget{
			action_kit_api.LineChartWidget{
				Type:  action_kit_api.ComSteadybitWidgetLineChart,
				Title: "HTTP Bandwidth",
				Identity: action_kit_api.LineChartWidgetIdentityConfig{
					MetricName: "bandwidth",
					From:       "url",
					Mode:       action_kit_api.ComSteadybitWidgetLineChartIdentityModeWidgetPerValue,
				},
				Grouping: new(action_kit_api.LineChartWidgetGroupingConfig{
					ShowSummary: new(true),
					Groups: []action_kit_api.LineChartWidgetGroup{
						{
							Title: "Within Threshold",
							Color: "success",
							Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
								Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
								Key:   "within_threshold",
								Value: "true",
							},
						},
						{
							Title: "Error",
							Color: "warn",
							Matcher: action_kit_api.LineChartWidgetGroupMatcherNotEmpty{
								Type: action_kit_api.ComSteadybitWidgetLineChartGroupMatcherNotEmpty,
								Key:  "error",
							},
						},
						{
							Title: "Outside Threshold",
							Color: "warn",
							Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
								Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
								Key:   "within_threshold",
								Value: "false",
							},
						},
					},
				}),
				Tooltip: new(action_kit_api.LineChartWidgetTooltipConfig{
					MetricValueTitle: new("Bandwidth"),
					MetricValueUnit:  new("Mbps"),
					AdditionalContent: []action_kit_api.LineChartWidgetTooltipContent{
						{
							From:  "bytes_downloaded",
							Title: "Bytes Downloaded",
						},
						{
							From:  "duration_ms",
							Title: "Window Duration (ms)",
						},
					},
				}),
			},
		})
	}

	description := action_kit_api.ActionDescription{
		Id:              ActionIDBandwidth,
		Label:           "HTTP Bandwidth",
		Description:     "Measures download bandwidth from an HTTP endpoint and validates against thresholds",
		Version:         extbuild.GetSemverVersionStringOrUnknown(),
		Icon:            new(actionIconBandwidth),
		TargetSelection: targetSelection,
		Widgets:         widgetToUse,

		Technology: new("HTTP"),
		Category:   new("HTTP"),

		Kind:        action_kit_api.Check,
		TimeControl: action_kit_api.TimeControlExternal,

		Parameters: []action_kit_api.ActionParameter{
			//------------------------
			// Request Definition
			//------------------------
			requestDefinition,
			urlParameter,
			headers,
			separator(5),
			//------------------------
			// Repetition Control
			//------------------------
			repetitionControl,
			{
				Name:         "maxConcurrent",
				Label:        "Concurrent Requests",
				Description:  new("Number of parallel requests to execute simultaneously without delay. More concurrent requests generate more traffic to saturate the available bandwidth."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: new("5"),
				Required:     new(true),
				Order:        new(7),
				MinValue:     new(1),
				MaxValue:     new(50),
			},
			duration,
			separator(9),
			//------------------------
			// Bandwidth Verification
			//------------------------
			{
				Name:  "bandwidthVerification",
				Label: "Bandwidth Verification",
				Type:  action_kit_api.ActionParameterTypeHeader,
				Order: new(10),
			},
			bandwidthSuccessRate,
			{
				Name:        "minBandwidth",
				Label:       "Minimum Bandwidth",
				Description: new("Minimum expected download bandwidth. Leave empty to skip minimum check."),
				Type:        action_kit_api.ActionParameterTypeBitrate,
				Required:    new(false),
				Order:       new(12),
			},
			{
				Name:        "maxBandwidth",
				Label:       "Maximum Bandwidth",
				Description: new("Maximum expected download bandwidth. Leave empty to skip maximum check."),
				Type:        action_kit_api.ActionParameterTypeBitrate,
				Required:    new(false),
				Order:       new(13),
			},
			separator(14),
			//------------------------
			// Target Selection
			//------------------------
			targetSelectionParameter,
			//------------------------
			// Client Settings
			//------------------------
			clientSettings,
			followRedirects,
			connectTimeout,
			readTimeout,
			insecureSkipVerify,
			failEarly,
		},
		Status: new(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: new("1s"),
		}),
		Stop: new(action_kit_api.MutatingEndpointReference{}),
	}

	if !config.Config.EnableLocationSelection {
		description.Parameters = filter(description.Parameters, func(p action_kit_api.ActionParameter) bool {
			return p.Type != action_kit_api.ActionParameterTypeTargetSelection
		})
		description.TargetSelection = nil
	}

	return description
}

func (a *httpCheckActionBandwidth) Prepare(_ context.Context, state *BandwidthCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	// Parse URL
	urlString, ok := request.Config["url"]
	if !ok {
		return nil, fmt.Errorf("URL is missing")
	}
	parsedURL, err := url.Parse(extutil.ToString(urlString))
	if err != nil {
		return nil, fmt.Errorf("URL could not be parsed: %w", err)
	}
	state.URL = *parsedURL

	// Parse headers
	state.Headers, err = extutil.ToKeyValue(request.Config, "headers")
	if err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}

	// Parse bandwidth thresholds
	minBandwidthStr := extutil.ToString(request.Config["minBandwidth"])
	maxBandwidthStr := extutil.ToString(request.Config["maxBandwidth"])

	if minBandwidthStr == "" && maxBandwidthStr == "" {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "At least one of minimum or maximum bandwidth must be specified",
			},
		}, nil
	}

	if minBandwidthStr != "" {
		state.MinBandwidthBps, err = parseBitrate(minBandwidthStr)
		if err != nil {
			return &action_kit_api.PrepareResult{
				Error: &action_kit_api.ActionKitError{
					Title: fmt.Sprintf("Invalid minimum bandwidth value: %s", err.Error()),
				},
			}, nil
		}
	}

	if maxBandwidthStr != "" {
		state.MaxBandwidthBps, err = parseBitrate(maxBandwidthStr)
		if err != nil {
			return &action_kit_api.PrepareResult{
				Error: &action_kit_api.ActionKitError{
					Title: fmt.Sprintf("Invalid maximum bandwidth value: %s", err.Error()),
				},
			}, nil
		}
	}

	if state.MinBandwidthBps > 0 && state.MaxBandwidthBps > 0 && state.MinBandwidthBps > state.MaxBandwidthBps {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "Minimum bandwidth cannot be greater than maximum bandwidth",
			},
		}, nil
	}

	// Parse other settings
	state.ExecutionID = request.ExecutionId
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	// Defaults to false to preserve the previous behavior (success rate evaluated only at the end).
	state.FailEarly = extutil.ToBool(request.Config["failEarly"])
	// One measurement window is emitted per status poll (~1s), so expected windows ~= duration in seconds.
	state.ExpectedWindows = uint64(extutil.ToInt64(request.Config["duration"]) / 1000)
	state.ConnectionTimeout = time.Duration(extutil.ToInt64(request.Config["connectTimeout"])) * time.Millisecond
	state.ReadTimeout = time.Duration(extutil.ToInt64(request.Config["readTimeout"])) * time.Millisecond
	state.FollowRedirects = extutil.ToBool(request.Config["followRedirects"])
	state.InsecureSkipVerify = extutil.ToBool(request.Config["insecureSkipVerify"])
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])
	if state.MaxConcurrent < 1 {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "Concurrent requests must be at least 1",
			},
		}, nil
	}

	bandwidthCheckers.Store(state.ExecutionID, newBandwidthChecker(state))

	return nil, nil
}

func (a *httpCheckActionBandwidth) Start(_ context.Context, state *BandwidthCheckState) (*action_kit_api.StartResult, error) {
	checker, err := loadBandwidthChecker(state.ExecutionID)
	if err != nil {
		return nil, err
	}
	checker.start()
	return nil, nil
}

func (a *httpCheckActionBandwidth) Status(_ context.Context, state *BandwidthCheckState) (*action_kit_api.StatusResult, error) {
	checker, err := loadBandwidthChecker(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load bandwidth checker")
		return nil, err
	}

	// Emit aggregated metric for the current measurement window and reset
	metric := checker.emitWindowMetric()

	var metrics []action_kit_api.Metric
	if metric != nil {
		metrics = append(metrics, *metric)
	}

	// Fail early if enabled and the required success rate can no longer be reached across the
	// expected number of measurement windows.
	if state.FailEarly {
		failed := checker.counterWindowFailed.Load()
		if successRateUnreachable(failed, state.ExpectedWindows, uint64(state.SuccessRate)) {
			return &action_kit_api.StatusResult{
				Completed: true,
				Metrics:   new(metrics),
				Error: &action_kit_api.ActionKitError{
					Title:  fmt.Sprintf("Success Rate can no longer reach %d%%", state.SuccessRate),
					Detail: new(fmt.Sprintf("%d of ~%d expected measurement windows already failed.", failed, state.ExpectedWindows)),
					Status: extutil.Ptr(action_kit_api.Failed),
				},
			}, nil
		}
	}

	return &action_kit_api.StatusResult{
		Completed: false,
		Metrics:   new(metrics),
	}, nil
}

func (a *httpCheckActionBandwidth) Stop(_ context.Context, state *BandwidthCheckState) (*action_kit_api.StopResult, error) {
	checker, err := loadBandwidthChecker(state.ExecutionID)
	if err != nil {
		log.Debug().Err(err).Msg("Bandwidth checker not found, stop was already called")
		return nil, nil
	}

	checker.stop()
	bandwidthCheckers.Delete(state.ExecutionID)

	// Emit final window metric
	var latestMetrics []action_kit_api.Metric
	if finalMetric := checker.emitWindowMetric(); finalMetric != nil {
		latestMetrics = append(latestMetrics, *finalMetric)
	}

	success := checker.counterWindowSuccess.Load()
	failed := checker.counterWindowFailed.Load()
	total := success + failed

	if total == 0 {
		return &action_kit_api.StopResult{
			Error: &action_kit_api.ActionKitError{
				Title:  "No measurement windows were completed",
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}, nil
	}

	// Guard against a target that delivers throughput but fails every request
	// (e.g. resets mid-body): such a run could otherwise pass on bandwidth alone.
	// Requiring an actual error avoids penalising a long download that is still in
	// flight at stop, where no request has completed yet but none has failed either.
	if checker.counterRequestsCompleted.Load() == 0 && checker.counterRequestsErrored.Load() > 0 {
		return &action_kit_api.StopResult{
			Metrics: &latestMetrics,
			Error: &action_kit_api.ActionKitError{
				Title:  "No HTTP requests completed successfully during the bandwidth check (the target is unreachable or failing all requests)",
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}, nil
	}

	successRate := float64(success) / float64(total) * 100.0

	log.Debug().Msgf("Bandwidth Check Success Rate: %.2f%% (%d of %d windows)", successRate, success, total)
	if successRate < float64(state.SuccessRate) {
		log.Info().Msgf("Success Rate (%.2f%%) was below %v%%", successRate, state.SuccessRate)
		return &action_kit_api.StopResult{
			Metrics: &latestMetrics,
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%.2f%%) was below %v%% (based on %d measurement windows)", successRate, state.SuccessRate, total),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}, nil
	}

	log.Info().Msgf("Success Rate (%.2f%%) was above/equal %v%%", successRate, state.SuccessRate)
	return &action_kit_api.StopResult{Metrics: &latestMetrics}, nil
}
