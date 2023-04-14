/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"strconv"
	"strings"
)

func toInt64(val interface{}) int64 {
	switch val.(type) {
	case int:
		return int64(val.(int))
	case int32:
		return int64(val.(int32))
	case int64:
		return val.(int64)
	case float32:
		return int64(val.(float32))
	case float64:
		return int64(val.(float64))
	default:
		return 0
	}
}

func toInt(val interface{}) int {
	switch val.(type) {
	case int:
		return val.(int)
	case int32:
		return int(val.(int32))
	case int64:
		return int(val.(int64))
	case float32:
		return int(val.(float32))
	case float64:
		return int(val.(float64))
	default:
		return 0
	}
}

func toString(val interface{}) string {
	if val == nil {
		return ""
	}
	return val.(string)
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

func toBool(val interface{}) bool {
	if val == nil {
		return false
	}
	return val.(bool)
}
func toKeyValue(request action_kit_api.PrepareActionRequestBody, configName string) (map[string]string, error) {
	kv, ok := request.Config[configName].([]any)
	if !ok {
		return nil, fmt.Errorf("failed to interpret config value for %s as a key/value array", configName)
	}

	result := make(map[string]string, len(kv))
	for _, rawEntry := range kv {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("failed to interpret config value for %s as a key/value array", configName)
		}
		result[entry["key"].(string)] = entry["value"].(string)
	}

	return result, nil
}
