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
	"fmt"
	"net/url"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/known"
)

var validateClusterNameRegex = regexp.MustCompile(nameFmt)
var validateClusterNamespaceRegex = regexp.MustCompile(namespaceFmt)
var validateServiceAccountTokenRegex = regexp.MustCompile(serviceAccountTokenFmt)

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

	// UseMetricsServer specifies whether to collect metrics from metrics server
	UseMetricsServer bool

	// LabelAggregateThreshold specifies the threshold of common node labels that will be aggregated to ManagedCluster
	// object in parent cluster. e.g. LabelAggregateThreshold 0.8 means if >=80% nodes in current child cluster have
	// such common labels, then these labels will be labeled to ManagedCluster object.
	LabelAggregateThreshold float32

	// LabelAggregatePrefix specifies the prefixes of node labels that should be aggregated
	LabelAggregatePrefix []string

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
		LabelAggregateThreshold:       0.8,
		LabelAggregatePrefix:          []string{known.NodeLabelsKeyPrefix},
	}
}

// AddFlagSets adds the flags to the flagsets.
func (opts *ClusterRegistrationOptions) AddFlagSets(fss *cliflag.NamedFlagSets) {
	crfs := fss.FlagSet("cluster registration")
	// flags for cluster registration
	crfs.StringVar(&opts.ParentURL, ClusterRegistrationURL, opts.ParentURL,
		"The parent cluster url you want to register to")
	crfs.StringVar(&opts.BootstrapToken, ClusterRegistrationToken, opts.BootstrapToken,
		"The boostrap token is used to temporarily authenticate with parent cluster while registering "+
			"a unregistered child cluster. On success, parent cluster credentials will be stored to a secret "+
			"in child cluster. On every restart, this credentials will be firstly used if found")
	crfs.StringVar(&opts.ClusterName, ClusterRegistrationName, opts.ClusterName,
		"Specify the cluster registration name")
	crfs.StringVar(&opts.ClusterNamePrefix, ClusterRegistrationNamePrefix, opts.ClusterNamePrefix,
		fmt.Sprintf("Specify a random cluster name with this prefix for registration if --%s is not specified",
			ClusterRegistrationName))
	crfs.StringVar(&opts.ClusterNamespace, ClusterRegistrationNamespace, opts.ClusterNamespace,
		"Specify the cluster registration namespace")
	crfs.StringVar(&opts.ClusterType, ClusterRegistrationType, opts.ClusterType,
		"Specify the cluster type")
	crfs.StringVar(&opts.ClusterSyncMode, ClusterSyncMode, opts.ClusterSyncMode,
		"Specify the sync mode 'Pull', 'Push' and 'Dual' between parent cluster and child cluster")

	mgmtfs := fss.FlagSet("cluster management")
	mgmtfs.StringVar(&opts.ClusterLabels, ClusterLabels, opts.ClusterLabels,
		"Specify the labels for the child cluster, split by `,`")
	mgmtfs.Float32Var(&opts.LabelAggregateThreshold, LabelAggregateThreshold, opts.LabelAggregateThreshold,
		"Specifies the threshold of common node labels that will be aggregated to ManagedCluster in parent cluster")
	mgmtfs.StringArrayVar(&opts.LabelAggregatePrefix, LabelAggregatePrefix, opts.LabelAggregatePrefix,
		"Specifies the desired prefix of node labels to be aggregated")

	mfs := fss.FlagSet("cluster health status")
	mfs.DurationVar(&opts.ClusterStatusReportFrequency.Duration, ClusterStatusReportFrequency, opts.ClusterStatusReportFrequency.Duration,
		"Specifies how often the agent posts current child cluster status to parent cluster")
	mfs.DurationVar(&opts.ClusterStatusCollectFrequency.Duration, ClusterStatusCollectFrequency, opts.ClusterStatusCollectFrequency.Duration,
		"Specifies how often the agent collects current child cluster status")
	mfs.BoolVar(&opts.UseMetricsServer, UseMetricsServer, opts.UseMetricsServer, "Use metrics server")
}

// Complete completes all the required options.
func (opts *ClusterRegistrationOptions) Complete() []error {
	var allErrs []error

	opts.ClusterName = strings.TrimSpace(opts.ClusterName)
	opts.ClusterNamePrefix = strings.TrimSpace(opts.ClusterNamePrefix)

	return allErrs
}

// Validate validates all the required options.
func (opts *ClusterRegistrationOptions) Validate() []error {
	var allErrs []error

	if len(opts.ParentURL) == 0 {
		klog.Exitf("please specify a parent cluster url by flag --%s", ClusterRegistrationURL)
	} else {
		_, err := url.ParseRequestURI(opts.ParentURL)
		if err != nil {
			allErrs = append(allErrs, fmt.Errorf("invalid value for --%s: %v", ClusterRegistrationURL, err))
		}
	}
	if len(opts.BootstrapToken) == 0 {
		klog.Exitf("please specify a token for parent cluster accessing by flag --%s", ClusterRegistrationToken)
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

	if len(opts.ClusterNamePrefix) > ClusterNameMaxLength-known.DefaultRandomIDLength-1 {
		allErrs = append(allErrs, fmt.Errorf("cluster name prefix %s is longer than %d",
			opts.ClusterName, ClusterNameMaxLength-known.DefaultRandomIDLength))
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

	// once getting registered, expired bootstrap tokens do no harm
	if !bootstraputil.IsValidBootstrapToken(opts.BootstrapToken) && !validateServiceAccountTokenRegex.MatchString(opts.BootstrapToken) {
		allErrs = append(allErrs, fmt.Errorf("the bootstrap token %q is invalid", opts.BootstrapToken))
	}

	return allErrs
}

var supportedClusterTypes = sets.NewString(
	string(clusterapi.StandardCluster),
	string(clusterapi.EdgeCluster),
)
