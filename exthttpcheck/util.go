/*
 * Copyright 2026 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
)

// resolveStatusCodeExpression resolves the given status code expression into a list of status codes
func resolveStatusCodeExpression(statusCodes string) ([]string, *action_kit_api.ActionKitError) {
	result := make([]string, 0)
	for _, code := range strings.Split(strings.Trim(statusCodes, " "), ";") {
		if strings.Contains(code, "-") {
			rangeParts := strings.Split(code, "-")
			if len(rangeParts) != 2 {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				return nil, &action_kit_api.ActionKitError{
					Title: fmt.Sprintf("Invalid status code range '%s'. Please use '-' for ranges and ';' for enumerations. Example: '200-399;429'", code),
				}
			}
			startCode, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				return nil, &action_kit_api.ActionKitError{
					Title: fmt.Sprintf("Invalid status code range '%s'. Please use '-' for ranges and ';' for enumerations. Example: '200-399;429'", code),
				}
			}
			endCode, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				log.Warn().Msgf("Invalid status code range '%s'", code)
				return nil, &action_kit_api.ActionKitError{
					Title: fmt.Sprintf("Invalid status code range '%s'. Please use '-' for ranges and ';' for enumerations. Example: '200-399;429'", code),
				}
			}
			for i := startCode; i <= endCode; i++ {
				if i < 100 || i > 599 {
					log.Warn().Msgf("Invalid status code '%d'", i)
					return nil, &action_kit_api.ActionKitError{
						Title: fmt.Sprintf("Invalid status code '%d'. Status code should be between 100 and 599.", i),
					}
				}
				result = append(result, strconv.Itoa(i))
			}
		} else {
			if len(code) == 0 {
				log.Error().Msgf("Invalid status code '%s'", code)
				return nil, &action_kit_api.ActionKitError{
					Title: "Status code is required.",
				}
			}
			if code == "error" {
				result = append(result, "error")
			} else {

				parsed, err := strconv.Atoi(code)
				if err != nil {
					log.Error().Msgf("Invalid status code '%s'", code)
					return nil, &action_kit_api.ActionKitError{
						Title: fmt.Sprintf("Invalid status code '%s'. Please use '-' for ranges and ';' for enumerations. Example: '200-399;429'", code),
					}
				}
				if parsed < 100 || parsed > 599 {
					log.Error().Msgf("Invalid status code '%d'", parsed)
					return nil, &action_kit_api.ActionKitError{
						Title: fmt.Sprintf("Invalid status code '%d'. Status code should be between 100 and 599.", parsed),
					}
				}
				result = append(result, strconv.Itoa(parsed))
			}
		}
	}
	return result, nil
}

// parseBitrate parses a bitrate string (e.g., "10mbit", "1024kbit", "1gbit") and returns bits per second
func parseBitrate(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty bitrate string")
	}

	s = strings.ToLower(strings.TrimSpace(s))

	// Regular expression to match number and unit
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(bit|kbit|mbit|gbit|tbit|bps|kbps|mbps|gbps|tbps)?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid bitrate format: %s", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid bitrate value: %s", matches[1])
	}

	unit := matches[2]
	if unit == "" {
		unit = "bit"
	}

	var multiplier float64
	switch unit {
	case "bit":
		multiplier = 1
	case "kbit":
		multiplier = 1000
	case "mbit":
		multiplier = 1_000_000
	case "gbit":
		multiplier = 1_000_000_000
	case "tbit":
		multiplier = 1_000_000_000_000
	case "bps":
		multiplier = 8
	case "kbps":
		multiplier = 8_000
	case "mbps":
		multiplier = 8_000_000
	case "gbps":
		multiplier = 8_000_000_000
	case "tbps":
		multiplier = 8_000_000_000_000
	default:
		return 0, fmt.Errorf("unknown bitrate unit: %s", unit)
	}

	return int64(value * multiplier), nil
}
