/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-http/config"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type httpCheckActionFixedAmount struct{}

// Make sure Action implements all required interfaces
var (
	_ action_kit_sdk.Action[HTTPCheckState]           = (*httpCheckActionFixedAmount)(nil)
	_ action_kit_sdk.ActionWithStatus[HTTPCheckState] = (*httpCheckActionFixedAmount)(nil)

	_ action_kit_sdk.ActionWithStop[HTTPCheckState] = (*httpCheckActionFixedAmount)(nil)
)

func NewHTTPCheckActionFixedAmount() action_kit_sdk.Action[HTTPCheckState] {
	return &httpCheckActionFixedAmount{}
}

func (l *httpCheckActionFixedAmount) NewEmptyState() HTTPCheckState {
	return HTTPCheckState{}
}

// Describe returns the action description for the platform with all required information.
func (l *httpCheckActionFixedAmount) Describe() action_kit_api.ActionDescription {

	widgetToUse := widgets
	if config.Config.EnableWidgetBackwardCompatibility {
		widgetToUse = widgetsBackwardCompatiblity
	}

	description := action_kit_api.ActionDescription{
		Id:              ActionIDFixedAmount,
		Label:           "HTTP (# of Requests)",
		Description:     "Calls an http endpoint a specified number of times and checks the response",
		Version:         extbuild.GetSemverVersionStringOrUnknown(),
		Icon:            extutil.Ptr(actionIconFixedAmount),
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
		TimeControl: action_kit_api.TimeControlInternal,

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
				Name:         "numberOfRequests",
				Label:        "Number of Requests.",
				Description:  extutil.Ptr("Fixed number of Requests, distributed to given duration"),
				Type:         action_kit_api.ActionParameterTypeInteger,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1"),
				Order:        extutil.Ptr(7),
			},
			duration,
			separator(9),
			//------------------------
			// Result Verification
			//------------------------
			resultVerification,
			successRate,
			statusCode,
			responsesContains,
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

func getDelayBetweenRequestsInMsFixedAmount(duration int64, numberOfRequests int64) int64 {
	if duration > 0 && numberOfRequests > 0 {
		return duration / (numberOfRequests)
	} else {
		return 1000 / 1
	}
}

func (l *httpCheckActionFixedAmount) Prepare(_ context.Context, state *HTTPCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	if extutil.ToInt64(request.Config["duration"]) == 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(extutil.ToInt64(request.Config["duration"]), extutil.ToInt64(request.Config["numberOfRequests"]))

	return prepare(request, state, checkEndedFixedAmount)
}

func checkEndedFixedAmount(executionRunData *ExecutionRunData, state *HTTPCheckState) bool {
	result := executionRunData.requestCounter.Load() >= state.NumberOfRequests
	return result
}

// Start is called to start the action
// You can mutate the state here.
// You can use the result to return messages/errors/metrics or artifacts
func (l *httpCheckActionFixedAmount) Start(_ context.Context, state *HTTPCheckState) (*action_kit_api.StartResult, error) {
	start(state)
	return nil, nil
}

// Status is called to get the current status of the action
func (l *httpCheckActionFixedAmount) Status(_ context.Context, state *HTTPCheckState) (*action_kit_api.StatusResult, error) {
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}

	completed := checkEndedFixedAmount(executionRunData, state)
	if completed {
		stopTickers(executionRunData)
		log.Info().Msg("Action completed")
	}

	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)

	return &action_kit_api.StatusResult{
		Completed: completed,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func (l *httpCheckActionFixedAmount) Stop(_ context.Context, state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

func (l *httpCheckActionFixedAmount) getExecutionRunData(executionID uuid.UUID) (*ExecutionRunData, error) {
	return loadExecutionRunData(executionID)
}
