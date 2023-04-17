/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
	"time"
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
	return action_kit_api.ActionDescription{
		Id:          targetIDFixedAmount,
		Label:       "HTTP Fixed Amount",
		Description: "Calls an http endpoint a specified number of times and checks the response",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(targetIconFixedAmount),
		Widgets: extutil.Ptr([]action_kit_api.Widget{
			action_kit_api.PredefinedWidget{
				Type:               action_kit_api.ComSteadybitWidgetPredefined,
				PredefinedWidgetId: "com.steadybit.widget.predefined.HttpCheck",
			},
		}),

		// Category for the targets to appear in
		Category: extutil.Ptr("HTTP"),

		// To clarify the purpose of the action:
		//   Check: Will perform checks on the targets
		Kind: action_kit_api.Check,

		// How the action is controlled over time.
		//   External: The agent takes care and calls stop then the time has passed. Requires a duration parameter. Use this when the duration is known in advance.
		//   Internal: The action has to implement the status endpoint to signal when the action is done. Use this when the duration is not known in advance.
		//   Instantaneous: The action is done immediately. Use this for actions that happen immediately, e.g. a reboot.
		TimeControl: action_kit_api.Internal,

		// The parameters for the action
		Parameters: []action_kit_api.ActionParameter{
			//------------------------
			// Request Definition
			//------------------------
			{
				Name:  "requestDefinition",
				Label: "Request Definition",
				Type:  action_kit_api.Header,
				Order: extutil.Ptr(0),
			},
			{
				Name:         "method",
				Label:        "HTTP Method",
				Description:  extutil.Ptr("The HTTP method to use."),
				Type:         action_kit_api.String,
				DefaultValue: extutil.Ptr("GET"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(1),
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{
						Label: "GET",
						Value: "GET",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "POST",
						Value: "POST",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "PUT",
						Value: "PUT",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "PATCH",
						Value: "PATCH",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "HEAD",
						Value: "HEAD",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "DELETE",
						Value: "DELETE",
					},
				}),
			},
			{
				Name:        "url",
				Label:       "Target URL",
				Description: extutil.Ptr("The URL to check."),
				Type:        action_kit_api.Url,
				Required:    extutil.Ptr(true),
				Order:       extutil.Ptr(2),
			},
			{
				Name:        "body",
				Label:       "HTTP Body",
				Description: extutil.Ptr("The HTTP Body."),
				Type:        action_kit_api.Textarea,
				Order:       extutil.Ptr(3),
			}, {
				Name:        "headers",
				Label:       "HTTP Headers",
				Description: extutil.Ptr("The HTTP Headers."),
				Type:        action_kit_api.KeyValue,
				Order:       extutil.Ptr(4),
			},
			{
				Name:  "-",
				Label: "-",
				Type:  action_kit_api.Separator,
				Order: extutil.Ptr(5),
			},
			//------------------------
			// Repitions
			//------------------------
			{
				Name:  "repetitionControl",
				Label: "Repetition Control",
				Type:  action_kit_api.Header,
				Order: extutil.Ptr(6),
			},
			{
				Name:         "numberOfRequests",
				Label:        "Number of Requests.",
				Description:  extutil.Ptr("Fixed number of Requests, distributed to given duration"),
				Type:         action_kit_api.Integer,
				Required:     extutil.Ptr(true),
				DefaultValue: extutil.Ptr("1"),
				Order:        extutil.Ptr(7),
			},
			{
				Name:         "duration",
				Label:        "Duration",
				Description:  extutil.Ptr("In which timeframe should the specified requests be executed?"),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("10s"),
				Required:     extutil.Ptr(false),
				Order:        extutil.Ptr(8),
			},
			{
				Name:  "-",
				Label: "-",
				Type:  action_kit_api.Separator,
				Order: extutil.Ptr(9),
			},
			//------------------------
			// Result Verification
			//------------------------
			{
				Name:  "resultVerification",
				Label: "Result Verification",
				Type:  action_kit_api.Header,
				Order: extutil.Ptr(10),
			},
			{
				Name:         "statusCode",
				Label:        "Response status codes",
				Description:  extutil.Ptr("Which HTTP-Status code should be considered as success? This field supports ranges with '-' and multiple codes delimited by ';' for example '200-399;429'."),
				Type:         action_kit_api.String,
				DefaultValue: extutil.Ptr("200-299"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(11),
			}, {
				Name:         "successRate",
				Label:        "Required Success Rate",
				Description:  extutil.Ptr("How many percent of the Request must be at least successful (in terms of the given response status codes above) to continue the experiment execution? The result will be evaluated and the end of the given duration."),
				Type:         action_kit_api.Percentage,
				DefaultValue: extutil.Ptr("100"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(12),
			}, {
				Name:        "responsesContains",
				Label:       "Responses contains",
				Description: extutil.Ptr("The Responses needs to contain the given string, otherwise the experiment will fail. The responses will be evaluated and the end of the given duration."),
				Type:        action_kit_api.String,
				Required:    extutil.Ptr(false),
				Order:       extutil.Ptr(13),
			},

			//------------------------
			// Additional Settings
			//------------------------

			{
				Name:        "followRedirects",
				Label:       "Follow Redirects?",
				Description: extutil.Ptr("Should Redirects be followed?"),
				Type:        action_kit_api.Boolean,
				Required:    extutil.Ptr(true),
				Advanced:    extutil.Ptr(true),
				Order:       extutil.Ptr(14),
			}, {
				Name:         "connectTimeout",
				Label:        "Connection Timeout",
				Description:  extutil.Ptr("Connection Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("5s"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(15),
			}, {
				Name:         "readTimeout",
				Label:        "Read Timeout",
				Description:  extutil.Ptr("Read Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("5s"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(16),
			},
			{
				Name:         "maxConcurrent",
				Label:        "Max concurrent requests",
				Description:  extutil.Ptr("Maximum count on parallel running requests. (min 1, max 10)"),
				Type:         action_kit_api.Integer,
				DefaultValue: extutil.Ptr("5"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(17),
			},
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}
}

func getDelayBetweenRequestsInMsFixedAmount(duration int64, numberOfRequests int64) int64 {
	if duration > 0 && numberOfRequests > 0 {
		return duration / (numberOfRequests)
	} else {
		return 1000 / 1
	}
}

func (l *httpCheckActionFixedAmount) Prepare(_ context.Context, state *HTTPCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMsFixedAmount(toInt64(request.Config["duration"]), toInt64(request.Config["numberOfRequests"]))

	result, err := prepare(request, state)
	if err != nil {
		return result, err
	}
	return nil, nil
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
	now := time.Now()
	executionRunData, err := loadExecutionRunData(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
		return nil, err
	}
	latestMetrics := retrieveLatestMetrics(executionRunData.metrics)
	completed := now.After(state.Timeout) || executionRunData.requestCounter >= state.NumberOfRequests

	if completed {
		stop(state)
		log.Debug().Msg("Action completed")
	}

	return &action_kit_api.StatusResult{
		Completed: completed,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func (l *httpCheckActionFixedAmount) Stop(_ context.Context, state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	return stop(state)
}
