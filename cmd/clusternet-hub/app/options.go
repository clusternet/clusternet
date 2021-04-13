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
)

// ClusterRegistrationOptions holds the command-line options for command
type options struct {
	// todo
}

// Complete completes all the required options.
func (opts *options) Complete() error {
	allErrs := []error{}

	// todo

	return utilerrors.NewAggregate(allErrs)
}

// Validate validates all the required options.
func (opts *options) Validate() error {
	allErrs := []error{}

	// todo

	return utilerrors.NewAggregate(allErrs)
}

// AddFlags adds the flags to the flagset.
func (opts *options) AddFlags(fs *pflag.FlagSet) {
	// todo
}

// NewOptions creates a new *options with sane defaults
func NewOptions() *options {
	return &options{
		// todo
	}
}
