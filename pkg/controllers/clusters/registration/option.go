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

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ClusterRegistrationOptions holds the command-line options about cluster registration
type ClusterRegistrationOptions struct {
	// ClusterName denotes the cluster name you want to register/display in parent cluster
	ClusterName string

	ClusterPrefix string

	ParentURL        string
	Token            string
	UnsafeParentCA   bool
	ParentKubeConfig string

	// TODO: check ca hash
}

// NewClusterRegistrationOptions creates a new *ClusterRegistrationOptions with sane defaults
func NewClusterRegistrationOptions() *ClusterRegistrationOptions {
	return &ClusterRegistrationOptions{
		ParentKubeConfig: DefaultParentKubeConfig,
		UnsafeParentCA:   true,
	}
}

// AddFlags adds the flags to the flagset.
func (opts *ClusterRegistrationOptions) AddFlags(fs *pflag.FlagSet) {
	// flags for cluster registration
	fs.StringVar(&opts.ParentURL, ClusterRegistrationURL, opts.ParentURL,
		"The parent cluster url you want to register to")
	fs.BoolVar(&opts.UnsafeParentCA, UnsafeParentCA, opts.UnsafeParentCA,
		"For token-based cluster registration, allowing registering without validating parent cluster CA")
	fs.StringVar(&opts.Token, ClusterRegistrationToken, opts.Token,
		fmt.Sprintf("If the file specified by --%s does not exist, this boostrap token is used to "+
			"temporarily authenticate with parent cluster while registering as a child cluster. "+
			"On success, a kubeconfig file of parent cluster will be written to the path specified by --%s "+
			"to avoid re-registering on every restart",
			ClusterRegistrationKubeconfig, ClusterRegistrationKubeconfig))
	fs.StringVar(&opts.ParentKubeConfig, ClusterRegistrationKubeconfig, opts.ParentKubeConfig,
		"Path to a kubeconfig file for parent cluster")
}

// Validate validates all the required options.
func (opts *ClusterRegistrationOptions) Validate() field.ErrorList {
	allErrs := field.ErrorList{}

	if len(opts.ParentKubeConfig) == 0 {
		if len(opts.ParentURL) == 0 {
			allErrs = append(allErrs, field.Required(field.NewPath(ClusterRegistrationURL),
				fmt.Sprintf("must be set if flag \"--%s\" is not specified", ClusterRegistrationKubeconfig)))
		}

		if len(opts.Token) == 0 {
			allErrs = append(allErrs, field.Required(field.NewPath(ClusterRegistrationToken),
				fmt.Sprintf("must be set if flag \"--%s\" is not specified", ClusterRegistrationKubeconfig)))
		}
	}

	return allErrs
}
