/*
 * Copyright 2023 steadybit GmbH. All rights reserved.
 */

package config

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"
)

// Specification is the configuration specification for the extension. Configuration values can be applied
// through environment variables. Learn more through the documentation of the envconfig package.
// https://github.com/kelseyhightower/envconfig
type Specification struct {
	KubernetesClusterName   string `json:"kubernetesClusterName" split_words:"true" required:"false"`
	KubernetesNodeName      string `json:"kubernetesNodeName" split_words:"true" required:"false"`
	KubernetesPodName       string `json:"kubernetesPodName" split_words:"true" required:"false"`
	KubernetesNamespace     string `json:"kubernetesNamespace" split_words:"true" required:"false"`
	EnableLocationSelection bool   `json:"enableLocationSelection" split_words:"true" required:"false"`
}

var (
	Config Specification
)

func ParseConfiguration() {
	err := envconfig.Process("steadybit_extension", &Config)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse configuration from environment.")
	}
}

func ValidateConfiguration() {
	// You may optionally validate the configuration here.
}
