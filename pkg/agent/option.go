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

package agent

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
)

var validateClusterNameRegex = regexp.MustCompile(nameFmt)
var validateClusterNamespaceRegex = regexp.MustCompile(namespaceFmt)

// ClusterRegistrationOptions holds the command-line options about cluster registration
type ClusterRegistrationOptions struct {
	// ClusterName denotes the cluster name you want to register/display in parent cluster
	ClusterName string
	// ClusterNamePrefix specifies the cluster name prefix for registration
	ClusterNamePrefix string
	// ClusterNamespace denotes the cluster namespace you want to register/display in parent cluster
	ClusterNamespace string
	// ClusterType denotes the cluster type
	ClusterType string
	// ClusterSyncMode specifies the sync mode between parent cluster and child cluster
	ClusterSyncMode string
	// ClusterLabels specifies the labels for the cluster
	ClusterLabels string

	// ClusterStatusReportFrequency is the frequency at which the agent reports current cluster's status
	ClusterStatusReportFrequency metav1.Duration
	// ClusterStatusCollectFrequency is the frequency at which the agent updates current cluster's status
	ClusterStatusCollectFrequency metav1.Duration

	ParentURL      string
	BootstrapToken string

	// No tunnel logging by default
	TunnelLogging bool

	// PredictorAddress specifies the address of predictor
	PredictorAddress string
	// PredictorDirectAccess indicates whether the predictor can be accessed directly by clusternet-scheduler
	PredictorDirectAccess bool
	// PredictorPort specifies the port on which to serve built-in predictor
	PredictorPort int
	// serveInternalPredictor indicates whether to serve built-in predictor. It is not a flag.
	serveInternalPredictor bool

	// UseMetricsServer specifies whether to collect metrics from metrics server
	UseMetricsServer bool

	// TODO: check ca hash
}

// NewClusterRegistrationOptions creates a new *ClusterRegistrationOptions with sane defaults
func NewClusterRegistrationOptions() *ClusterRegistrationOptions {
	return &ClusterRegistrationOptions{
		ClusterNamePrefix:             RegistrationNamePrefix,
		ClusterType:                   string(clusterapi.StandardCluster),
		ClusterSyncMode:               string(clusterapi.Pull),
		ClusterStatusReportFrequency:  metav1.Duration{Duration: DefaultClusterStatusReportFrequency},
		ClusterStatusCollectFrequency: metav1.Duration{Duration: DefaultClusterStatusCollectFrequency},
		PredictorPort:                 8080,
		PredictorDirectAccess:         false,
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
	fs.StringVar(&opts.ClusterNamespace, ClusterRegistrationNamespace, opts.ClusterNamespace,
		"Specify the cluster registration namespace")
	fs.StringVar(&opts.ClusterType, ClusterRegistrationType, opts.ClusterType,
		"Specify the cluster type")
	fs.StringVar(&opts.ClusterSyncMode, ClusterSyncMode, opts.ClusterSyncMode,
		"Specify the sync mode 'Pull', 'Push' and 'Dual' between parent cluster and child cluster")
	fs.StringVar(&opts.ClusterLabels, ClusterLabels, opts.ClusterLabels,
		"Specify the labels for the child cluster, split by `,`")
	fs.DurationVar(&opts.ClusterStatusReportFrequency.Duration, ClusterStatusReportFrequency, opts.ClusterStatusReportFrequency.Duration,
		"Specifies how often the agent posts current child cluster status to parent cluster")
	fs.DurationVar(&opts.ClusterStatusCollectFrequency.Duration, ClusterStatusCollectFrequency, opts.ClusterStatusCollectFrequency.Duration,
		"Specifies how often the agent collects current child cluster status")
	fs.BoolVar(&opts.TunnelLogging, "enable-tunnel-logging", opts.TunnelLogging, "Enable tunnel logging")
	fs.BoolVar(&opts.UseMetricsServer, UseMetricsServer, opts.UseMetricsServer, "Use metrics server")
	fs.StringVar(&opts.PredictorAddress, PredictorAddress, opts.PredictorAddress,
		"Set address of external predictor, such as https://abc.com:8080. If not set, built-in predictor will be used when feature gate 'Predictor' is enabled.")
	fs.BoolVar(&opts.PredictorDirectAccess, PredictorDirectAccess, opts.PredictorDirectAccess,
		"Whether the predictor be accessed directly by clusternet-scheduler")
	fs.IntVar(&opts.PredictorPort, PredictorPort, opts.PredictorPort,
		"Set port on which to serve built-in predictor server. It is only used when feature gate 'Predictor' is enabled and '--predictor-addr' is not set.")
}

// Complete completes all the required options.
func (opts *ClusterRegistrationOptions) Complete() []error {
	allErrs := []error{}

	opts.ClusterName = strings.TrimSpace(opts.ClusterName)
	opts.ClusterNamePrefix = strings.TrimSpace(opts.ClusterNamePrefix)

	if utilfeature.DefaultFeatureGate.Enabled(features.Predictor) && opts.PredictorAddress == "" {
		opts.PredictorAddress = fmt.Sprintf("http://localhost:%d", opts.PredictorPort)
		opts.serveInternalPredictor = true
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

	if len(opts.ClusterName) > 0 {
		if len(opts.ClusterName) > ClusterNameMaxLength {
			allErrs = append(allErrs, fmt.Errorf("cluster name %s is longer than %d", opts.ClusterName, ClusterNameMaxLength))
		}

		if !validateClusterNameRegex.MatchString(opts.ClusterName) {
			allErrs = append(allErrs,
				fmt.Errorf("invalid name for --%s, regex used for validation is %q", ClusterRegistrationName, nameFmt))
		}
	}

	if len(opts.ClusterNamespace) > 0 {
		if len(opts.ClusterNamespace) > ClusterNamespaceMaxLength {
			allErrs = append(allErrs, fmt.Errorf("cluster namespace %s is longer than %d", opts.ClusterNamespace, ClusterNamespaceMaxLength))
		}

		if !validateClusterNamespaceRegex.MatchString(opts.ClusterNamespace) {
			allErrs = append(allErrs,
				fmt.Errorf("invalid namespace for --%s, regex used for validation is %q", ClusterRegistrationNamespace, namespaceFmt))
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

	switch opts.ClusterSyncMode {
	case string(clusterapi.Push):
		if !utilfeature.DefaultFeatureGate.Enabled(features.AppPusher) {
			allErrs = append(allErrs,
				fmt.Errorf("inconsitent setting: FeatureGate %s is disbled, while syncMode is set to %s",
					features.AppPusher, opts.ClusterSyncMode))
		}
	case string(clusterapi.Pull), string(clusterapi.Dual):
	default:
		allErrs = append(allErrs, fmt.Errorf("invalid sync mode %q, only 'Pull', 'Push' and 'Dual' are supported", opts.ClusterSyncMode))
	}

	// TODO: check bootstrap token

	return allErrs
}

var supportedClusterTypes = sets.NewString(
	string(clusterapi.StandardCluster),
	string(clusterapi.EdgeCluster),
)
