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
	"regexp"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

var validateClusterNameRegex = regexp.MustCompile(nameFmt)

// ClusterRegistrationOptions holds the command-line options about cluster registration
type ClusterRegistrationOptions struct {
	// ClusterName denotes the cluster name you want to register/display in parent cluster
	ClusterName string
	// ClusterNamePrefix specifies the cluster name prefix for registration
	ClusterNamePrefix string
	// ClusterType denotes the cluster type
	ClusterType string

	ParentURL      string
	BootstrapToken string

	// TODO: check ca hash
}

// NewClusterRegistrationOptions creates a new *ClusterRegistrationOptions with sane defaults
func NewClusterRegistrationOptions() *ClusterRegistrationOptions {
	return &ClusterRegistrationOptions{
		ClusterNamePrefix: RegistrationNamePrefix,
		ClusterType:       string(clusterapi.EdgeClusterSelfProvisioned),
	}
}

// AddFlags adds the flags to the flagset.
func (opts *ClusterRegistrationOptions) AddFlags(fs *pflag.FlagSet) {
	// flags for cluster registration
	fs.StringVar(&opts.ParentURL, ClusterRegistrationURL, opts.ParentURL,
		"The parent cluster url you want to register to")
	fs.StringVar(&opts.BootstrapToken, ClusterRegistrationToken, opts.BootstrapToken,
		"The boostrap token is used to temporarily authenticate with parent cluster while registering "+
			"a unregistered child cluster. On success, parent cluster credentials will be stored to a secret "+
			"in child cluster. On every restart, this credentials will be firstly used if found")
	fs.StringVar(&opts.ClusterName, ClusterRegistrationName, opts.ClusterName,
		"Specify the cluster registration name")
	fs.StringVar(&opts.ClusterNamePrefix, ClusterRegistrationNamePrefix, opts.ClusterNamePrefix,
		fmt.Sprintf("Specify a random cluster name with this prefix for registration if --%s is not specified",
			ClusterRegistrationName))
	fs.StringVar(&opts.ClusterType, ClusterRegistrationType, opts.ClusterType,
		"Specify the cluster type")
}

// Complete completes all the required options.
func (opts *ClusterRegistrationOptions) Complete() []error {
	allErrs := []error{}

	opts.ClusterName = strings.TrimSpace(opts.ClusterName)
	opts.ClusterNamePrefix = strings.TrimSpace(opts.ClusterNamePrefix)

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

	if len(opts.ClusterName) > 0 {
		if len(opts.ClusterName) > ClusterNameMaxLength {
			allErrs = append(allErrs, fmt.Errorf("cluster name %s is longer than %d", opts.ClusterName, ClusterNameMaxLength))
		}

		if !validateClusterNameRegex.MatchString(opts.ClusterName) {
			allErrs = append(allErrs,
				fmt.Errorf("invalid name for --%s, regex used for validation is %q", ClusterRegistrationName, nameFmt))
		}
	}

	if len(opts.ClusterType) > 0 && !supportedClusterTypes.Has(opts.ClusterType) {
		allErrs = append(allErrs, fmt.Errorf("invalid cluster type %q, please specify one from %s",
			opts.ClusterType, supportedClusterTypes.List()))
	}

	if len(opts.ClusterNamePrefix) > ClusterNameMaxLength-DefaultRandomUIDLength-1 {
		allErrs = append(allErrs, fmt.Errorf("cluster name prefix %s is longer than %d",
			opts.ClusterName, ClusterNameMaxLength-DefaultRandomUIDLength))
	}

	// TODO: check bootstrap token

	return allErrs
}

var supportedClusterTypes = sets.NewString(
	string(clusterapi.EdgeClusterSelfProvisioned),
	// todo: add more types
)
