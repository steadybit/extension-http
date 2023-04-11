/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type httpCheckAction struct{}

// Make sure Action implements all required interfaces
var (
	_ action_kit_sdk.Action[HttpCheckState]           = (*httpCheckAction)(nil)
	_ action_kit_sdk.ActionWithStatus[HttpCheckState] = (*httpCheckAction)(nil)

	_ action_kit_sdk.ActionWithStop[HttpCheckState] = (*httpCheckAction)(nil)

	jobs    = map[uuid.UUID]chan time.Time{}             // stores the jobs for each execution
	tickers = map[uuid.UUID]*time.Ticker{}               // stores the tickers for each execution, to be able to stop them
	metrics = map[uuid.UUID]chan action_kit_api.Metric{} // stores the metrics for each execution

)

type HttpCheckState struct {
	ExpectedStatusCodes  []int
	DelayBetweenRequests int
	Timeout              time.Time
	ResponsesContains    string
	SuccessRate          float64
	MaxConcurrent        int
	NumberOfRequests     int
	RequestsPerSecond    int
	ReadTimeout          time.Duration
	ExecutionId          uuid.UUID
}

func NewHttpCheckAction() action_kit_sdk.Action[HttpCheckState] {
	return &httpCheckAction{}
}

func (l *httpCheckAction) NewEmptyState() HttpCheckState {
	return HttpCheckState{}
}

// Describe returns the action description for the platform with all required information.
func (l *httpCheckAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s", targetID),
		Label:       "check http",
		Description: "calls a http endpoint and checks the response",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(targetIcon),
		// The target type this action is for
		//TargetType: extutil.Ptr(targetID),

		// Category for the targets to appear in
		Category: extutil.Ptr("checks"),

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
				Name:         "method",
				Label:        "HTTP Method",
				Description:  extutil.Ptr("The HTTP method to use."),
				Type:         action_kit_api.String,
				DefaultValue: extutil.Ptr("GET"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(0),
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{
						Label: "GET",
						Value: "get",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "POST",
						Value: "post",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "PUT",
						Value: "put",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "PATCH",
						Value: "patch",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "HEAD",
						Value: "head",
					},
					action_kit_api.ExplicitParameterOption{
						Label: "DELETE",
						Value: "delete",
					},
				}),
			},
			{
				Name:        "url",
				Label:       "Target URL",
				Description: extutil.Ptr("The URL to check."),
				Type:        "uri",
				Required:    extutil.Ptr(true),
				Order:       extutil.Ptr(1),
			},
			{
				Name:        "body",
				Label:       "HTTP Body",
				Description: extutil.Ptr("The HTTP Body."),
				Type:        "textarea",
				Order:       extutil.Ptr(2),
			}, {
				Name:        "headers",
				Label:       "HTTP Headers",
				Description: extutil.Ptr("The HTTP Headers."),
				Type:        action_kit_api.KeyValue,
				Order:       extutil.Ptr(3),
			}, {
				Name:        "followRedirects",
				Label:       "Follow Redirects?",
				Description: extutil.Ptr("Should Redirects be followed?"),
				Type:        action_kit_api.Boolean,
				Required:    extutil.Ptr(true),
				Advanced:    extutil.Ptr(true),
				Order:       extutil.Ptr(4),
			}, {
				Name:         "connectTimeout",
				Label:        "Connection Timeout",
				Description:  extutil.Ptr("Connection Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("5s"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(5),
			}, {
				Name:         "readTimeout",
				Label:        "Read Timeout",
				Description:  extutil.Ptr("Read Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("5s"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(6),
			},
			//------------------------
			// Repitions
			//------------------------
			{
				Name:         "requestsPerSecond",
				Label:        "Requests per second",
				Description:  extutil.Ptr("The number of requests per second. Should be between 1 and 10."),
				Type:         action_kit_api.Integer,
				DefaultValue: extutil.Ptr("1"),
				Required:     extutil.Ptr(false),
				Advanced:     extutil.Ptr(false),
				Order:        extutil.Ptr(7),
			}, {
				Name:        "numberOfRequests",
				Label:       "Number of Requests.",
				Description: extutil.Ptr("Fixed number of Requests, distributed to given duration"),
				Type:        action_kit_api.Integer,
				Required:    extutil.Ptr(false),
				Advanced:    extutil.Ptr(false),
				Order:       extutil.Ptr(8),
			}, {
				Name:         "duration",
				Label:        "Duration",
				Description:  extutil.Ptr("In which timeframe should the specified requests be executed?"),
				Type:         action_kit_api.Duration,
				DefaultValue: extutil.Ptr("10s"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(false),
				Order:        extutil.Ptr(8),
			}, {
				Name:         "maxConcurrent",
				Label:        "Max concurrent requests",
				Description:  extutil.Ptr("Maximum count on parallel running requests. (min 1, max 10)"),
				Type:         action_kit_api.Integer,
				DefaultValue: extutil.Ptr("5"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(true),
				Order:        extutil.Ptr(9),
			},
			//------------------------
			// Result Verification
			//------------------------
			{
				Name:         "statusCode",
				Label:        "Response status codes",
				Description:  extutil.Ptr("Which HTTP-Status code should be considered as success? This field supports ranges with '-' and multiple codes delimited by ';' for example '200-399;429'."),
				Type:         action_kit_api.String,
				DefaultValue: extutil.Ptr("200-299"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(false),
				Order:        extutil.Ptr(10),
			}, {
				Name:         "successRate",
				Label:        "Required Success Rate",
				Description:  extutil.Ptr("How many percent of the Request must be at least successful (in terms of the given response status codes above) to continue the experiment execution? The result will be evaluated and the end of the given duration."),
				Type:         action_kit_api.Percentage,
				DefaultValue: extutil.Ptr("100"),
				Required:     extutil.Ptr(true),
				Advanced:     extutil.Ptr(false),
				Order:        extutil.Ptr(11),
			}, {
				Name:        "responsesContains",
				Label:       "Responses contains",
				Description: extutil.Ptr("The Responses needs to contain the given string, otherwise the experiment will fail. The responses will be evaluated and the end of the given duration."),
				Type:        action_kit_api.String,
				Required:    extutil.Ptr(false),
				Advanced:    extutil.Ptr(false),
				Order:       extutil.Ptr(12),
			},
		},
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("1s"),
		}),
		Stop: extutil.Ptr(action_kit_api.MutatingEndpointReference{}),
	}
}

func (l *httpCheckAction) Prepare(_ context.Context, state *HttpCheckState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	duration := math.Round(request.Config["duration"].(float64))
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(duration))
	expectedStatusCodes, err := resolveStatusCodeExpression(request.Config["statusCode"].(string))
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve status codes")
		return nil, err
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.DelayBetweenRequests = getDelayBetweenRequests(request.Config["duration"].(int), request.Config["requestsPerSecond"].(int), request.Config["numberOfRequests"].(int))
	state.ResponsesContains = request.Config["responsesContains"].(string)
	state.SuccessRate = request.Config["successRate"].(float64)
	state.MaxConcurrent = int(request.Config["maxConcurrent"].(float64))
	state.NumberOfRequests = int(request.Config["numberOfRequests"].(float64))
	state.RequestsPerSecond = int(request.Config["requestsPerSecond"].(float64))
	state.ReadTimeout = time.Duration(request.Config["readTimeout"].(float64)) * time.Second
	state.ExecutionId = request.ExecutionId

	metrics[state.ExecutionId] = make(chan action_kit_api.Metric, state.MaxConcurrent)

	req, err := http.NewRequest(http.MethodGet, "https://www.google.com", nil)

	jobs[state.ExecutionId] = make(chan time.Time, state.MaxConcurrent)
	metrics[state.ExecutionId] = make(chan action_kit_api.Metric, state.MaxConcurrent)
	for w := 1; w <= state.MaxConcurrent; w++ {
		go requestWorker(req, jobs[state.ExecutionId], metrics[state.ExecutionId])
	}

	return nil, nil
}

func requestWorker(req *http.Request, jobs chan time.Time, results chan action_kit_api.Metric) {
	for range jobs {
		//start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Error().Err(err).Msg("Failed to execute request")
		}
		log.Info().Msgf("Response: %v", resp)
		//elapsed := time.Since(start)
		results <- action_kit_api.Metric{
			Metric: map[string]string{
				"__name__": "latency",
			},
			Value: 42,
		}
	}
}

func getDelayBetweenRequests(duration int, requestsPerSecond int, numberOfRequests int) int {
	if duration > 0 && numberOfRequests > 0 {
		return duration / numberOfRequests
	} else {
		if requestsPerSecond > 0 {
			return 1000 / requestsPerSecond
		} else {
			return 1000 / 1
		}
	}
}

// resolveStatusCodeExpression resolves the given status code expression into a list of status codes
func resolveStatusCodeExpression(statusCodes string) ([]int, error) {
	result := make([]int, 0)
	for _, code := range strings.Split(strings.Trim(statusCodes, " "), ";") {
		if strings.Contains(code, "-") {
			rangeParts := strings.Split(code, "-")
			if len(rangeParts) != 2 {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				continue
			}
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				continue
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				continue
			}
			for i := start; i <= end; i++ {
				if i > 599 {
					log.Warn().Msgf("Invalid status code '%d'", i)
					return nil, fmt.Errorf("invalid status code '%d'", i)
				}
				result = append(result, i)
			}
		} else {
			code, err := strconv.Atoi(code)
			if err != nil {
				log.Warn().Msgf("Invalid status code '%s'", code)
				continue
			}
			if code > 599 {
				log.Warn().Msgf("Invalid status code '%d'", code)
				return nil, fmt.Errorf("invalid status code '%d'", code)
			}
			result = append(result, code)
		}
	}
	return result, nil
}

// Start is called to start the action
// You can mutate the state here.
// You can use the result to return messages/errors/metrics or artifacts
func (l *httpCheckAction) Start(_ context.Context, state *HttpCheckState) (*action_kit_api.StartResult, error) {
	ticker := tickers[state.ExecutionId]
	ticker = time.NewTicker(time.Duration(state.DelayBetweenRequests) * time.Second)
	go func() {
		for t := range ticker.C {
			jobs[state.ExecutionId] <- t
		}
	}()

	return nil, nil
}

// Status is called to get the current status of the action
func (l *httpCheckAction) Status(_ context.Context, state *HttpCheckState) (*action_kit_api.StatusResult, error) {

	return &action_kit_api.StatusResult{
		//indicate that the action is still running
		Completed: false,
		Metrics: extutil.Ptr([]action_kit_api.Metric{
			{
				Timestamp: time.Now(),
				Metric: map[string]string{
					"__name__": "latency",
				},
				Value: 42,
			},
		}),
	}, nil
}

func (l *httpCheckAction) Stop(ctx context.Context, state *HttpCheckState) (*action_kit_api.StopResult, error) {
	ticker := tickers[state.ExecutionId]
	ticker.Stop()
	return nil, nil
}
