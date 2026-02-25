/*
 * Copyright 2026 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-http/config"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type httpCheckActionFixedAmount struct{}

var (
	_ action_kit_sdk.Action[HTTPCheckState]           = (*httpCheckActionFixedAmount)(nil)
	_ action_kit_sdk.ActionWithStatus[HTTPCheckState] = (*httpCheckActionFixedAmount)(nil)
	_ action_kit_sdk.ActionWithStop[HTTPCheckState]   = (*httpCheckActionFixedAmount)(nil)
)

func NewHTTPCheckActionFixedAmount() action_kit_sdk.Action[HTTPCheckState] {
	return &httpCheckActionFixedAmount{}
}

func (l *httpCheckActionFixedAmount) NewEmptyState() HTTPCheckState {
	return HTTPCheckState{}
}

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
		Technology:      extutil.Ptr("HTTP"),
		Kind:            action_kit_api.Check,
		TimeControl:     action_kit_api.TimeControlInternal,
		Hint: &action_kit_api.ActionHint{
			Content: "Please note that the given number of requests is uniformly distributed over the given duration. For example, 10 requests in 10 seconds " +
				"will result in 1 request per second, whereas the first request is executed immediately.  " +
				"The requests are handled by the given number of parallel processes, adhering to the overall request count.",
			Type: action_kit_api.HintInfo,
		},
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
			// Repetitions
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
				MinValue:     extutil.Ptr(1),
			},
			{
				Name:         "duration",
				Label:        "Duration",
				Description:  extutil.Ptr("In which timeframe should the specified requests be executed?"),
				Type:         action_kit_api.ActionParameterTypeDuration,
				DefaultValue: extutil.Ptr("2s"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(8),
			},
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

func (l *httpCheckActionFixedAmount) Prepare(_ context.Context, state *HTTPCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	duration := time.Duration(extutil.ToInt64(request.Config["duration"])) * time.Millisecond
	if duration <= 0 {
		return nil, errors.New("duration must be greater than 0")
	}
	numberOfRequests := extutil.ToUInt64(request.Config["numberOfRequests"])
	if numberOfRequests == 0 {
		return nil, errors.New("number of requests must be greater than 0")
	} else if numberOfRequests == 1 {
		state.DelayBetweenRequests = 24 * time.Hour // high value to only execute initial request
	} else {
		state.DelayBetweenRequests = time.Duration(uint64(duration) / numberOfRequests)
	}
	if state.DelayBetweenRequests < time.Millisecond {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "The given Number of Requests is too high for the given duration. Please reduce the number of requests or increase the duration.",
			},
		}, nil
	}

	return prepare(request, state)
}

func (l *httpCheckActionFixedAmount) Start(_ context.Context, state *HTTPCheckState) (*action_kit_api.StartResult, error) {
	return start(state)
}

// Status is called to get the current status of the action
func (l *httpCheckActionFixedAmount) Status(_ context.Context, state *HTTPCheckState) (*action_kit_api.StatusResult, error) {
	return status(state)
}

func (l *httpCheckActionFixedAmount) Stop(_ context.Context, state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

func (l *httpCheckActionFixedAmount) getHttpChecker(executionID uuid.UUID) (*httpChecker, error) {
	return loadHttpChecker(executionID)
}
