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
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/clusternet/clusternet/pkg/controllers/clusters/registration"
)

const (
	// kubeConfig flag sets the kubeconfig file to use when talking to current child cluster.
	kubeConfig = "kubeconfig"
)

// ClusterRegistrationOptions holds the command-line options for command
type options struct {
	kubeconfig          string
	clusterRegistration *registration.ClusterRegistrationOptions
}

// Validate validates all the required options.
func (opts *options) Validate() error {
	allErrs := field.ErrorList{}

	// validate cluster registration options
	errs := opts.clusterRegistration.Validate()
	allErrs = append(allErrs, errs...)

	return allErrs.ToAggregate()
}

// AddFlags adds the flags to the flagset.
func (opts *options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&opts.kubeconfig, kubeConfig, opts.kubeconfig,
		"Path to a kubeconfig file for current child cluster. Only required if out-of-cluster")

	// flags for cluster registration
	opts.clusterRegistration.AddFlags(fs)
}

// NewOptions creates a new *options with sane defaults
func NewOptions() *options {
	return &options{
		clusterRegistration: registration.NewClusterRegistrationOptions(),
	}
}
