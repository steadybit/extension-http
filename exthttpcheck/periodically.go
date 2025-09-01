/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-http/config"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type httpCheckActionPeriodically struct{}

// Make sure Action implements all required interfaces
var (
	_ action_kit_sdk.Action[HTTPCheckState]           = (*httpCheckActionPeriodically)(nil)
	_ action_kit_sdk.ActionWithStatus[HTTPCheckState] = (*httpCheckActionPeriodically)(nil)

	_ action_kit_sdk.ActionWithStop[HTTPCheckState] = (*httpCheckActionPeriodically)(nil)
)

func NewHTTPCheckActionPeriodically() action_kit_sdk.Action[HTTPCheckState] {
	return &httpCheckActionPeriodically{}
}

func (l *httpCheckActionPeriodically) NewEmptyState() HTTPCheckState {
	return HTTPCheckState{}
}

// Describe returns the action description for the platform with all required information.
func (l *httpCheckActionPeriodically) Describe() action_kit_api.ActionDescription {
	widgetToUse := widgets
	if config.Config.EnableWidgetBackwardCompatibility {
		widgetToUse = widgetsBackwardCompatiblity
	}

	description := action_kit_api.ActionDescription{
		Id:              ActionIDPeriodically,
		Label:           "HTTP (Requests / s)",
		Description:     "Calls a http endpoint periodically (requests / s) and checks the response",
		Version:         extbuild.GetSemverVersionStringOrUnknown(),
		Icon:            extutil.Ptr(actionIconPeriodically),
		TargetSelection: targetSelection,
		Widgets:         widgetToUse,

		Technology: extutil.Ptr("HTTP"),
		Category:   extutil.Ptr("HTTP"), //Can be removed in Q1/24 - support for backward compatibility of old sidebar

		// To clarify the purpose of the action:
		//   Check: Will perform checks on the targets
		Kind: action_kit_api.Check,

		// How the action is controlled over time.
		//   External: The agent takes care and calls stop then the time has passed. Requires a duration parameter. Use this when the duration is known in advance.
		//   Internal: The action has to implement the status endpoint to signal when the action is done. Use this when the duration is not known in advance.
		//   Instantaneous: The action is done immediately. Use this for actions that happen immediately, e.g. a reboot.
		TimeControl: action_kit_api.TimeControlExternal,

		// The parameters for the action
		Parameters: []action_kit_api.ActionParameter{
			//------------------------
			// Request Definition
			//------------------------
			requestDefinition,
			method,
			urlParameter,
			body,
			headers,
			separator(5),
			//------------------------
			// Repitions
			//------------------------
			repetitionControl,
			{
				Name:         "requestsPerSecond",
				Label:        "Requests per second",
				Description:  extutil.Ptr("The number of requests per second. Should be between 1 and 10."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: extutil.Ptr("1"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(7),
				MinValue:     extutil.Ptr(1),
			},
			duration,
			separator(9),
			//------------------------
			// Result Verification
			//------------------------
			resultVerification,
			successRate,
			statusCode,
			responsesContain,
			responseTimeMode,
			responseTime,
			separator(16),

			//------------------------
			// Target Selection
			//------------------------
			targetSelectionParameter,

			//------------------------
			// Additional Settings
			//------------------------

			maxConcurrent,
			clientSettings,
			followRedirects,
			connectTimeout,
			readTimeout,
			insecureSkipVerify,
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}

	if !config.Config.EnableLocationSelection {
		description.Parameters = filter(description.Parameters, func(p action_kit_api.ActionParameter) bool {
			return p.Type != action_kit_api.ActionParameterTypeTargetSelection
		})
		description.TargetSelection = nil
	}

	return description
}

func getDelayBetweenRequestsInMsPeriodically(requestsPerSecond uint64) uint64 {
	if requestsPerSecond > 0 {
		return 1000 / requestsPerSecond
	} else {
		return 1000 / 1
	}
}

func (l *httpCheckActionPeriodically) Prepare(_ context.Context, state *HTTPCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	state.RequestsPerSecond = extutil.ToUInt64(request.Config["requestsPerSecond"])
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsPeriodically(state.RequestsPerSecond)
	if state.DelayBetweenRequestsInMS < 1 {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "The given Number of Requests is too high for the given duration. Please reduce the number of requests or increase the duration.",
			},
		}, nil
	}
	return prepare(request, state, nil)
}

// Start is called to start the action
// You can mutate the state here.
// You can use the result to return messages/errors/metrics or artifacts
func (l *httpCheckActionPeriodically) Start(_ context.Context, state *HTTPCheckState) (*action_kit_api.StartResult, error) {
	start(state)
	return nil, nil
}

// Status is called to get the current status of the action
func (l *httpCheckActionPeriodically) Status(_ context.Context, state *HTTPCheckState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadHttpChecker(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}
	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	return &action_kit_api.StatusResult{
		Completed: false,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func (l *httpCheckActionPeriodically) Stop(_ context.Context, state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

func (l *httpCheckActionPeriodically) getExecutionRunData(executionID uuid.UUID) (*httpChecker, error) {
	return loadHttpChecker(executionID)
}
