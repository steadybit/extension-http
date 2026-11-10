// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package exthttpcheck

import (
	"testing"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupMembers(desc action_kit_api.ActionDescription) map[string][]string {
	members := map[string][]string{}
	for _, p := range desc.Parameters {
		if p.Group != nil {
			members[*p.Group] = append(members[*p.Group], p.Name)
		}
	}
	return members
}

func advancedParameters(desc action_kit_api.ActionDescription) []string {
	var names []string
	for _, p := range desc.Parameters {
		if p.Advanced != nil && *p.Advanced {
			names = append(names, p.Name)
		}
	}
	return names
}

func parameterByName(t *testing.T, desc action_kit_api.ActionDescription, name string) action_kit_api.ActionParameter {
	for _, p := range desc.Parameters {
		if p.Name == name {
			return p
		}
	}
	require.Failf(t, "parameter not found", "%s", name)
	return action_kit_api.ActionParameter{}
}

// Only groups whose description adds something beyond the title are defined.
func definedGroups(desc action_kit_api.ActionDescription) []string {
	var names []string
	if desc.ParameterGroups != nil {
		for _, g := range *desc.ParameterGroups {
			names = append(names, g.Name)
		}
	}
	return names
}

func TestCheckActions_ParameterGroups(t *testing.T) {
	for name, desc := range map[string]action_kit_api.ActionDescription{
		"periodically": NewHTTPCheckActionPeriodically().Describe(),
		"fixedAmount":  NewHTTPCheckActionFixedAmount().Describe(),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []string{groupResponse}, definedGroups(desc))
			assert.Equal(t, map[string][]string{
				groupRequestContent:     {"headers", "body"},
				groupResponse:           {"statusCode", "responsesContains", "responseTimeMode", "responseTime", "responseTimeMeasurement", "failEarly"},
				groupHTTPClientBehavior: {"maxConcurrent", "keepAlive", "followRedirects", "connectTimeout", "readTimeout", "insecureSkipVerify"},
			}, groupMembers(desc))

			// Platforms without groups keep showing exactly these in "Additional Settings".
			assert.ElementsMatch(t, []string{"maxConcurrent", "keepAlive", "followRedirects", "connectTimeout", "readTimeout", "insecureSkipVerify", "failEarly"}, advancedParameters(desc))

			assert.Less(t, *parameterByName(t, desc, "headers").Order, *parameterByName(t, desc, "body").Order, "headers come before the body")
			for _, p := range desc.Parameters {
				assert.NotEqual(t, "clientSettings", p.Name, "the group title replaces the header")
			}
		})
	}
}

func TestBandwidthAction_ParameterGroups(t *testing.T) {
	desc := NewHTTPCheckActionBandwidth().Describe()

	assert.Empty(t, definedGroups(desc))
	assert.Equal(t, map[string][]string{
		groupHTTPClientBehavior: {"keepAlive", "followRedirects", "connectTimeout", "readTimeout", "insecureSkipVerify"},
	}, groupMembers(desc))
	assert.ElementsMatch(t, []string{"keepAlive", "followRedirects", "connectTimeout", "readTimeout", "insecureSkipVerify"}, advancedParameters(desc))

	// Fail early is the last bandwidth verification setting, not alone in "Additional Settings".
	failEarlyOrder := *parameterByName(t, desc, "failEarly").Order
	assert.Greater(t, failEarlyOrder, *parameterByName(t, desc, "maxBandwidth").Order)
	for _, p := range desc.Parameters {
		if p.Type == action_kit_api.ActionParameterTypeSeparator && *p.Order > *parameterByName(t, desc, "maxBandwidth").Order {
			assert.Greater(t, *p.Order, failEarlyOrder, "the separator after the verification settings follows fail early")
			break
		}
	}
}
