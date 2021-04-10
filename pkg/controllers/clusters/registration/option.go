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

package registration

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/pflag"
)

// ClusterRegistrationOptions holds the command-line options about cluster registration
type ClusterRegistrationOptions struct {
	// ClusterName denotes the cluster name you want to register/display in parent cluster
	ClusterName string
	// ClusterNamePrefix specifies the cluster name prefix for registration
	ClusterNamePrefix string

	ParentURL      string
	BootstrapToken string
	UnsafeParentCA bool

	// TODO: check ca hash
}

// NewClusterRegistrationOptions creates a new *ClusterRegistrationOptions with sane defaults
func NewClusterRegistrationOptions() *ClusterRegistrationOptions {
	return &ClusterRegistrationOptions{
		UnsafeParentCA:    true,
		ClusterNamePrefix: RegistrationNamePrefix,
	}
}

// AddFlags adds the flags to the flagset.
func (opts *ClusterRegistrationOptions) AddFlags(fs *pflag.FlagSet) {
	// flags for cluster registration
	fs.StringVar(&opts.ParentURL, ClusterRegistrationURL, opts.ParentURL,
		"The parent cluster url you want to register to")
	fs.BoolVar(&opts.UnsafeParentCA, ClusterRegistrationUnsafeParentCA, opts.UnsafeParentCA,
		"For token-based cluster registration, allowing registering without validating parent cluster CA")
	fs.StringVar(&opts.BootstrapToken, ClusterRegistrationToken, opts.BootstrapToken,
		"The boostrap token is used to temporarily authenticate with parent cluster while registering "+
			"a unregistered child cluster. On success, parent cluster credentials will be stored to a secret "+
			"in child cluster. On every restart, this credentials will be firstly used if found.")
	fs.StringVar(&opts.ClusterName, ClusterRegistrationName, opts.ClusterName,
		"Specify the cluster registration name.")
	fs.StringVar(&opts.ClusterNamePrefix, ClusterRegistrationNamePrefix, opts.ClusterNamePrefix,
		fmt.Sprintf("Specify a random cluster name with this prefix for registration if --%s is not specified",
			ClusterRegistrationName))
}

// Complete completes all the required options.
func (opts *ClusterRegistrationOptions) Complete() []error {
	allErrs := []error{}

	opts.ClusterNamePrefix = strings.TrimSpace(opts.ClusterNamePrefix)
	if !strings.HasSuffix(opts.ClusterNamePrefix, "-") {
		allErrs = append(allErrs, fmt.Errorf(`wrong value for --%s, which should ends with "-"`, ClusterRegistrationNamePrefix))
	}

	return allErrs
}

// Validate validates all the required options.
func (opts *ClusterRegistrationOptions) Validate() []error {
	allErrs := []error{}

	if len(opts.ParentURL) > 0 {
		_, err := url.ParseRequestURI(opts.ParentURL)
		if err != nil {
			allErrs = append(allErrs, fmt.Errorf("invalid value for --%s: %v", ClusterRegistrationURL, err))
		}
	}

	// TODO: check bootstrap token

	return allErrs
}
