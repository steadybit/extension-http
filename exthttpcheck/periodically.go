/*
 * Copyright 2026 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-http/config"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type httpCheckActionPeriodically struct{}

var (
	_ action_kit_sdk.Action[HTTPCheckState]           = (*httpCheckActionPeriodically)(nil)
	_ action_kit_sdk.ActionWithStatus[HTTPCheckState] = (*httpCheckActionPeriodically)(nil)
	_ action_kit_sdk.ActionWithStop[HTTPCheckState]   = (*httpCheckActionPeriodically)(nil)
)

func NewHTTPCheckActionPeriodically() action_kit_sdk.Action[HTTPCheckState] {
	return &httpCheckActionPeriodically{}
}

func (l *httpCheckActionPeriodically) NewEmptyState() HTTPCheckState {
	return HTTPCheckState{}
}

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
		Icon:            new(actionIconPeriodically),
		TargetSelection: targetSelection,
		Widgets:         widgetToUse,
		Technology:      new("HTTP"),
		Kind:            action_kit_api.Check,
		TimeControl:     action_kit_api.TimeControlExternal,
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
				Name:         "requestsPerSecond",
				Label:        "Requests per second",
				Description:  new("The number of requests per second. Should be between 1 and 10."),
				Type:         action_kit_api.ActionParameterTypeInteger,
				DefaultValue: new("1"),
				Required:     new(true),
				Order:        new(7),
				MinValue:     new(1),
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
			insecureSkipVerify,
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

func getDelayBetweenRequests(requestsPerSecond uint64) time.Duration {
	if requestsPerSecond > 0 {
		return time.Duration(uint64(time.Second) / requestsPerSecond)
	}
	return time.Second
}

func (l *httpCheckActionPeriodically) Prepare(_ context.Context, state *HTTPCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	requestsPerSecond := extutil.ToUInt64(request.Config["requestsPerSecond"])
	state.DelayBetweenRequests = getDelayBetweenRequests(requestsPerSecond)
	if state.DelayBetweenRequests < time.Millisecond {
		return &action_kit_api.PrepareResult{
			Error: &action_kit_api.ActionKitError{
				Title: "The given Number of Requests is too high for the given duration. Please reduce the number of requests or increase the duration.",
			},
		}, nil
	}
	return prepare(request, state)
}

func (l *httpCheckActionPeriodically) Start(_ context.Context, state *HTTPCheckState) (*action_kit_api.StartResult, error) {
	return start(state)
}

func (l *httpCheckActionPeriodically) Status(_ context.Context, state *HTTPCheckState) (*action_kit_api.StatusResult, error) {
	return status(state)
}

func (l *httpCheckActionPeriodically) Stop(_ context.Context, state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	return stop(state)
}

func (l *httpCheckActionPeriodically) getExecutionRunData(executionID uuid.UUID) (*httpChecker, error) {
	return loadHttpChecker(executionID)
}
