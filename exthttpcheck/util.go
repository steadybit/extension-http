/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package exthttpcheck

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"strconv"
	"strings"
)

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
				log.Warn().Msgf("Invalid status code '%d'", code)
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
