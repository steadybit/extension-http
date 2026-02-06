/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
)

var (
	httpCheckers = sync.Map{} //make(map[uuid.UUID]*httpChecker)
)

type HTTPCheckState struct {
	ExpectedStatusCodes      []string
	DelayBetweenRequestsInMS uint64
	Timeout                  time.Time
	ResponsesContains        string
	SuccessRate              int
	ResponseTimeMode         string
	ResponseTime             *time.Duration
	MaxConcurrent            int
	NumberOfRequests         uint64
	RequestsPerSecond        uint64
	ReadTimeout              time.Duration
	ExecutionID              uuid.UUID
	Body                     string
	URL                      url.URL
	Method                   string
	Headers                  map[string]string
	ConnectionTimeout        time.Duration
	FollowRedirects          bool
	InsecureSkipVerify       bool
}

func prepare(request action_kit_api.PrepareActionRequestBody, state *HTTPCheckState, checkEnded checkEndedFn) (*action_kit_api.PrepareResult, error) {
	duration := extutil.ToInt64(request.Config["duration"])
	state.Timeout = time.Now().Add(time.Millisecond * time.Duration(duration))
	expectedStatusCodes, statusCodeErr := resolveStatusCodeExpression(extutil.ToString(request.Config["statusCode"]))
	if statusCodeErr != nil {
		return &action_kit_api.PrepareResult{
			Error: statusCodeErr,
		}, nil
	}
	state.ExpectedStatusCodes = expectedStatusCodes
	state.ResponsesContains = extutil.ToString(request.Config["responsesContains"])
	state.SuccessRate = extutil.ToInt(request.Config["successRate"])
	state.ResponseTimeMode = extutil.ToString(request.Config["responseTimeMode"])
	state.ResponseTime = extutil.Ptr(time.Duration(extutil.ToInt64(request.Config["responseTime"])) * time.Millisecond)
	state.MaxConcurrent = extutil.ToInt(request.Config["maxConcurrent"])
	state.NumberOfRequests = extutil.ToUInt64(request.Config["numberOfRequests"])
	state.ReadTimeout = time.Duration(extutil.ToInt64(request.Config["readTimeout"])) * time.Millisecond
	state.ExecutionID = request.ExecutionId
	state.Body = extutil.ToString(request.Config["body"])
	state.Method = extutil.ToString(request.Config["method"])
	state.ConnectionTimeout = time.Duration(extutil.ToInt64(request.Config["connectTimeout"])) * time.Millisecond
	state.FollowRedirects = extutil.ToBool(request.Config["followRedirects"])
	state.InsecureSkipVerify = extutil.ToBool(request.Config["insecureSkipVerify"])
	var err error
	state.Headers, err = extutil.ToKeyValue(request.Config, "headers")
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse headers")
		return nil, err
	}

	urlString, ok := request.Config["url"]
	if !ok {
		return nil, fmt.Errorf("URL is missing")
	}
	parsedUrl, err := url.Parse(extutil.ToString(urlString))
	if err != nil {
		log.Error().Err(err).Msg("URL could not be parsed missing")
		return nil, err
	}
	state.URL = *parsedUrl

	checker := newHttpChecker(state, checkEnded)
	httpCheckers.Store(state.ExecutionID, checker)

	return nil, nil
}

func loadHttpChecker(executionID uuid.UUID) (*httpChecker, error) {
	item, ok := httpCheckers.Load(executionID)
	if !ok {
		return nil, fmt.Errorf("failed to load associated http checker")
	}
	return item.(*httpChecker), nil
}

func start(state *HTTPCheckState) {
	checker, err := loadHttpChecker(state.ExecutionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load execution run data")
	}

	checker.start()
}

func loadAndDeleteHttpChecker(id uuid.UUID) (*httpChecker, error) {
	item, ok := httpCheckers.LoadAndDelete(id)
	if !ok {
		return nil, fmt.Errorf("failed to load associated http checker")
	}
	return item.(*httpChecker), nil
}

func stop(state *HTTPCheckState) (*action_kit_api.StopResult, error) {
	checker, err := loadAndDeleteHttpChecker(state.ExecutionID)
	if err != nil {
		log.Debug().Err(err).Msg("Execution run data not found, stop was already called")
		return nil, nil
	}

	checker.stop()

	latestMetrics := checker.getLatestMetrics()
	success := checker.counters.success.Load()
	failed := checker.counters.failed.Load()
	total := success + failed

	result := action_kit_api.StopResult{Metrics: &latestMetrics}

	if successRate := float64(success) / float64(total) * 100.0; successRate >= float64(state.SuccessRate) {
		log.Info().Msgf("Success Rate %.2f%% (%d of %d) was greater or equal than %d%%", successRate, success, failed, state.SuccessRate)
	} else {
		log.Info().Msgf("Success Rate %.2f%% (%d of %d) was less than %d%%", successRate, success, failed, state.SuccessRate)
		result.Error = &action_kit_api.ActionKitError{
			Title:  fmt.Sprintf("Success Rate (%.2f%%) was below %d%%", successRate, state.SuccessRate),
			Detail: extutil.Ptr(fmt.Sprintf("%d of %d requests were successful.", success, total)),
			Status: extutil.Ptr(action_kit_api.Failed),
		}
	}

	return &result, nil
}
