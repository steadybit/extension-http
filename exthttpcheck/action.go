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
	"golang.org/x/exp/slices"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
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

	stopTicker = map[uuid.UUID]chan bool{}                  // stores the stop channels for each execution
	jobs       = map[uuid.UUID]chan time.Time{}             // stores the jobs for each execution
	tickers    = map[uuid.UUID]*time.Ticker{}               // stores the tickers for each execution, to be able to stop them
	metrics    = map[uuid.UUID]chan action_kit_api.Metric{} // stores the metrics for each execution

	requestCounter        = map[uuid.UUID]int{} // stores the number of requests for each execution
	requestSuccessCounter = map[uuid.UUID]int{} // stores the number of successful requests for each execution

)

type HttpCheckState struct {
	ExpectedStatusCodes      []int
	DelayBetweenRequestsInMS int64
	Timeout                  time.Time
	ResponsesContains        string
	SuccessRate              int
	MaxConcurrent            int
	NumberOfRequests         int
	RequestsPerSecond        int
	ReadTimeout              time.Duration
	ExecutionId              uuid.UUID
	Body                     string
	Url                      string
	Method                   string
	Headers                  map[string]string
	ConnectionTimeout        time.Duration
	FollowRedirects          bool
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
		Label:       "HTTP Endpoint",
		Description: "Calls a http endpoint and checks the response",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(targetIcon),
		Widgets: extutil.Ptr([]action_kit_api.Widget{
			action_kit_api.PredefinedWidget{
				Type:               action_kit_api.ComSteadybitWidgetPredefined,
				PredefinedWidgetId: "com.steadybit.widget.predefined.HttpCheck",
			},
		}),

		// Category for the targets to appear in
		Category: extutil.Ptr("Http"),

		// To clarify the purpose of the action:
		//   Check: Will perform checks on the targets
		Kind: action_kit_api.Check,

		// How the action is controlled over time.
		//   External: The agent takes care and calls stop then the time has passed. Requires a duration parameter. Use this when the duration is known in advance.
		//   Internal: The action has to implement the status endpoint to signal when the action is done. Use this when the duration is not known in advance.
		//   Instantaneous: The action is done immediately. Use this for actions that happen immediately, e.g. a reboot.
		TimeControl: action_kit_api.External,

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
				DefaultValue: extutil.Ptr("get"),
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
				Type:        action_kit_api.EnvVarOrUri,
				Required:    extutil.Ptr(true),
				Order:       extutil.Ptr(1),
			},
			{
				Name:        "body",
				Label:       "HTTP Body",
				Description: extutil.Ptr("The HTTP Body."),
				Type:        action_kit_api.Textarea,
				Order:       extutil.Ptr(2),
			}, {
				Name:        "headers",
				Label:       "HTTP Headers",
				Description: extutil.Ptr("The HTTP Headers."),
				Type:        action_kit_api.KeyValue,
				Order:       extutil.Ptr(3),
			},
			{
				Name:  "-",
				Label: "-",
				Type:  action_kit_api.Separator,
				Order: extutil.Ptr(4),
			},
			{
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
	expectedStatusCodes, err := resolveStatusCodeExpression(toString(request.Config["statusCode"]))
	if err != nil {
		log.Error().Err(err).Msg("Failed to resolve status codes")
		return nil, err
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.DelayBetweenRequestsInMS = getDelayBetweenRequestsInMs(toInt64(request.Config["duration"]), toInt64(request.Config["requestsPerSecond"]), toInt64(request.Config["numberOfRequests"]))
	state.ResponsesContains = toString(request.Config["responsesContains"])
	state.SuccessRate = toInt(request.Config["successRate"])
	state.MaxConcurrent = toInt(request.Config["maxConcurrent"])
	state.NumberOfRequests = toInt(request.Config["numberOfRequests"])
	state.RequestsPerSecond = toInt(request.Config["requestsPerSecond"])
	state.ReadTimeout = time.Duration(toInt64(request.Config["readTimeout"])) * time.Second
	state.ExecutionId = request.ExecutionId
	state.Body = toString(request.Config["body"])
	state.Url = toString(request.Config["url"])
	state.Method = toString(request.Config["method"])
	state.ConnectionTimeout = time.Duration(toInt64(request.Config["connectTimeout"])) * time.Second
	state.FollowRedirects = toBool(request.Config["followRedirects"])
	state.Headers, err = toKeyValue(request, "headers")
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse headers")
		return nil, err
	}

	metrics[state.ExecutionId] = make(chan action_kit_api.Metric, state.MaxConcurrent)

	req, err := createRequest(state)

	// create job channel
	jobs[state.ExecutionId] = make(chan time.Time, state.MaxConcurrent)
	// create metrics result channel
	metrics[state.ExecutionId] = make(chan action_kit_api.Metric, state.MaxConcurrent)
	// create worker pool
	for w := 1; w <= state.MaxConcurrent; w++ {
		go requestWorker(req, jobs[state.ExecutionId], metrics[state.ExecutionId], state)
	}

	return nil, nil
}

func createRequest(state *HttpCheckState) (*http.Request, error) {
	var body io.Reader = nil
	if state.Body != "" {
		body = strings.NewReader(state.Body)
	}
	var method = "GET"
	if state.Method != "" {
		method = state.Method
	}

	request, err := http.NewRequest(strings.ToUpper(method), state.Url, body)
	if err != nil {
		for k, v := range state.Headers {
			request.Header.Add(k, v)
		}
	}
	return request, err
}

func requestWorker(req *http.Request, jobs chan time.Time, results chan action_kit_api.Metric, state *HttpCheckState) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: state.ConnectionTimeout,
		}).DialContext,
	}
	client := http.Client{Timeout: state.ReadTimeout, Transport: transport}

	if !state.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	for range jobs {
		var start = time.Now()
		var elapsed time.Duration

		trace := &httptrace.ClientTrace{
			WroteRequest: func(info httptrace.WroteRequestInfo) {
				start = time.Now()
			},
			GotFirstResponseByte: func() {
				elapsed = time.Since(start)
			},
		}

		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		log.Debug().Msgf("Requesting %s", req.URL.String())
		response, err := client.Do(req)

		requestCounter[state.ExecutionId]++
		responseStatusWasExpected := false
		responseBodyWasSuccessful := true

		if err != nil {
			log.Error().Err(err).Msg("Failed to execute request")
			results <- action_kit_api.Metric{
				Metric: map[string]string{
					"url":   req.URL.String(),
					"error": err.Error(),
				},
				Name:      extutil.Ptr("response_time"),
				Value:     float64(time.Since(start).Milliseconds()),
				Timestamp: time.Now(),
			}
			responseStatusWasExpected = false
			return
		}
		log.Debug().Msgf("Got response %s", response.Status)
		responseStatusWasExpected = slices.Contains(state.ExpectedStatusCodes, response.StatusCode)
		metricMap := map[string]string{
			"url":                  req.URL.String(),
			"http_status":          strconv.Itoa(response.StatusCode),
			"expected_http_status": strconv.FormatBool(responseStatusWasExpected),
		}
		if state.ResponsesContains != "" {
			if response.Body == nil {
				metricMap["response_constraints_fulfilled"] = strconv.FormatBool(false)
				responseBodyWasSuccessful = false
			} else {
				bodyBytes, err := io.ReadAll(response.Body)
				if err != nil {
					log.Error().Err(err).Msg("Failed to read response body")
					metricMap["response_constraints_fulfilled"] = strconv.FormatBool(false)
					responseBodyWasSuccessful = false
				} else {
					bodyString := string(bodyBytes)
					metricMap["response_constraints_fulfilled"] = strconv.FormatBool(strings.Contains(bodyString, state.ResponsesContains))
					responseBodyWasSuccessful = true
				}
			}
		}
		if responseStatusWasExpected && responseBodyWasSuccessful {
			requestSuccessCounter[state.ExecutionId]++
		}
		metric := action_kit_api.Metric{
			Name:      extutil.Ptr("response_time"),
			Metric:    metricMap,
			Value:     float64(elapsed.Milliseconds()),
			Timestamp: time.Now(),
		}
		results <- metric
	}
}

// Start is called to start the action
// You can mutate the state here.
// You can use the result to return messages/errors/metrics or artifacts
func (l *httpCheckAction) Start(_ context.Context, state *HttpCheckState) (*action_kit_api.StartResult, error) {
	tickers[state.ExecutionId] = time.NewTicker(time.Duration(state.DelayBetweenRequestsInMS) * time.Millisecond)
	stopTicker[state.ExecutionId] = make(chan bool)
	now := time.Now()
	log.Debug().Msgf("Schedule first Request at %v", now)
	jobs[state.ExecutionId] <- now
	go func() {
		for {
			select {
			case <-stopTicker[state.ExecutionId]:
				return
			case t := <-tickers[state.ExecutionId].C:
				log.Debug().Msgf("Schedule Request at %v", t)
				jobs[state.ExecutionId] <- t
			}
		}
	}()

	return nil, nil
}

// Status is called to get the current status of the action
func (l *httpCheckAction) Status(_ context.Context, state *HttpCheckState) (*action_kit_api.StatusResult, error) {
	now := time.Now()
	latestMetrics := retrieveLatestMetrics(state.ExecutionId)
	completed := now.After(state.Timeout)

	if completed {
		log.Debug().Msg("Action completed")
	}
	return &action_kit_api.StatusResult{
		Completed: false,
		Metrics:   extutil.Ptr(latestMetrics),
	}, nil
}

func retrieveLatestMetrics(executionId uuid.UUID) []action_kit_api.Metric {
	statusMetrics := make([]action_kit_api.Metric, 0, len(metrics[executionId]))
	for {
		select {
		case metric, ok := <-metrics[executionId]:
			if ok {
				log.Debug().Msgf("Status Metric: %v", metric)
				statusMetrics = append(statusMetrics, metric)
			} else {
				log.Debug().Msg("Channel closed")
				return statusMetrics
			}
		default:
			log.Debug().Msg("No metrics available")
			return statusMetrics
		}
	}
	return statusMetrics
}

func (l *httpCheckAction) Stop(ctx context.Context, state *HttpCheckState) (*action_kit_api.StopResult, error) {
	ticker := tickers[state.ExecutionId]
	if ticker != nil {
		ticker.Stop()
	}
	stopTicker[state.ExecutionId] <- true // stop the ticker

	//get latest metrics
	latestMetrics := retrieveLatestMetrics(state.ExecutionId)
	// calculate the success rate
	successRate := float64(requestSuccessCounter[state.ExecutionId]) / float64(requestCounter[state.ExecutionId]) * 100
	log.Debug().Msgf("Success Rate: %f", successRate)
	if successRate < float64(state.SuccessRate) {
		return extutil.Ptr(action_kit_api.StopResult{
			Metrics: extutil.Ptr(latestMetrics),
			Error: &action_kit_api.ActionKitError{
				Title:  fmt.Sprintf("Success Rate (%v) was below %v%", successRate, state.SuccessRate),
				Status: extutil.Ptr(action_kit_api.Failed),
			},
		}), nil
	}
	return extutil.Ptr(action_kit_api.StopResult{
		Metrics: extutil.Ptr(latestMetrics),
	}), nil
}
