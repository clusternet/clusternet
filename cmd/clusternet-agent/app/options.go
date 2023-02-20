/*
Copyright 2021 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"github.com/spf13/pflag"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/clusternet/clusternet/pkg/agent"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// ClusterRegistrationOptions holds the command-line options for command
type options struct {
	clusterRegistration *agent.ClusterRegistrationOptions
	*utils.ControllerOptions
}

// Complete completes all the required options.
func (opts *options) Complete() error {
	allErrs := []error{}

	// complete cluster registration options
	errs := opts.clusterRegistration.Complete()
	allErrs = append(allErrs, errs...)

	// complete leader election and client connection options
	if err := opts.ControllerOptions.Complete(); err != nil {
		allErrs = append(allErrs, err)
	}

	return utilerrors.NewAggregate(allErrs)
}

// Validate validates all the required options.
func (opts *options) Validate() error {
	allErrs := []error{}

	// validate cluster registration options
	errs := opts.clusterRegistration.Validate()
	allErrs = append(allErrs, errs...)

	// validate leader election and client connection options
	if err := opts.ControllerOptions.Validate(); err != nil {
		allErrs = append(allErrs, err)
	}

	return utilerrors.NewAggregate(allErrs)
}

// AddFlags adds the flags to the flagset.
func (opts *options) AddFlags(fs *pflag.FlagSet) {
	// flags for cluster registration
	opts.clusterRegistration.AddFlags(fs)

	// flags for leader election and client connection
	opts.ControllerOptions.AddFlags(fs)
}

// NewOptions creates a new *options with sane defaults
func NewOptions() (*options, error) {
	controllerOptions, err := utils.NewControllerOptions("clusternet-agent", known.ClusternetSystemNamespace)
	if err != nil {
		return nil, err
	}

	return &options{
		clusterRegistration: agent.NewClusterRegistrationOptions(),
		ControllerOptions:   controllerOptions,
	}, nil
}
