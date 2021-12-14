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

package options

import (
	"github.com/spf13/pflag"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// SchedulerOptions has all the params needed to run a Scheduler
type SchedulerOptions struct {
	*utils.ControllerOptions
}

// NewSchedulerOptions returns a new SchedulerOptions
func NewSchedulerOptions() (*SchedulerOptions, error) {
	controllerOptions, err := utils.NewControllerOptions("clusternet-scheduler", known.ClusternetSystemNamespace)
	if err != nil {
		return nil, err
	}

	return &SchedulerOptions{
		ControllerOptions: controllerOptions,
	}, nil
}

// Validate validates SchedulerOptions
func (o *SchedulerOptions) Validate() error {
	errors := []error{}

	// validate leader election and client connection options
	if err := o.ControllerOptions.Validate(); err != nil {
		errors = append(errors, err)
	}

	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *SchedulerOptions) Complete() error {
	allErrs := []error{}

	// complete leader election and client connection options
	if err := o.ControllerOptions.Complete(); err != nil {
		allErrs = append(allErrs, err)
	}

	return utilerrors.NewAggregate(allErrs)
}

// AddFlags adds flags for SchedulerOptions.
func (o *SchedulerOptions) AddFlags(fs *pflag.FlagSet) {
	o.ControllerOptions.AddFlags(fs)
}
