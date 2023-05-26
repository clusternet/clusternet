/*
Copyright 2023 The Clusternet Authors.

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
	"net"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/metrics"
	controllermanageroptions "k8s.io/controller-manager/options"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// DefaultClusternetControllerManagerPort is the default port for the clusternet-controller-manager running in parent cluster.
	// May be overridden by a flag at startup.
	DefaultClusternetControllerManagerPort = 10660
)

// ControllerManagerOptions holds the command-line options for command
type ControllerManagerOptions struct {

	// Whether the anonymous access is allowed by the kube-apiserver,
	// i.e. flag "--anonymous-auth=true" is set to kube-apiserver.
	// If enabled, then the deployers in Clusternet will use anonymous when proxying requests to child clusters.
	// If not, serviceaccount "clusternet-hub-proxy" will be used instead.
	AnonymousAuthSupported bool
	// default namespace to create Manifest in
	// default to be "clusternet-reserved"
	ReservedNamespace    string
	ClusterAPIKubeconfig string

	//Threadiness of controller workers, default to be 10
	Threadiness int

	// Controllers is the list of controllers to enable or disable
	// '*' means "all enabled by default controllers"
	// 'foo' means "enable 'foo'"
	// '-foo' means "disable 'foo'"
	// first item for a particular name wins
	Controllers []string

	*utils.ControllerOptions

	SecureServing *apiserveroptions.SecureServingOptionsWithLoopback

	// DebuggingOptions holds the Debugging options.
	DebuggingOptions *controllermanageroptions.DebuggingOptions

	Metrics *metrics.Options
}

// NewControllerManagerOptions creates a new *options with sane defaults
func NewControllerManagerOptions() (*ControllerManagerOptions, error) {
	controllerOptions, err := utils.NewControllerOptions("clusternet-controller-manager", known.ClusternetSystemNamespace)
	if err != nil {
		return nil, err
	}

	opts := &ControllerManagerOptions{
		SecureServing:          apiserveroptions.NewSecureServingOptions().WithLoopback(),
		DebuggingOptions:       controllermanageroptions.RecommendedDebuggingOptions(),
		ControllerOptions:      controllerOptions,
		Metrics:                metrics.NewOptions(),
		AnonymousAuthSupported: true,
		ReservedNamespace:      known.ClusternetReservedNamespace,
		Threadiness:            known.DefaultThreadiness,
	}
	// Set the PairName but leave certificate directory blank to generate in-memory by default
	opts.SecureServing.ServerCert.CertDirectory = ""
	opts.SecureServing.ServerCert.PairName = "clusternet-controller-manager"
	opts.SecureServing.BindPort = DefaultClusternetControllerManagerPort
	//opts.initFlags()
	return opts, nil
}

func (o *ControllerManagerOptions) Config() error {
	if o.SecureServing != nil {
		if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
			return fmt.Errorf("error creating self-signed certificates: %v", err)
		}
	}

	o.Metrics.Apply()
	return nil
}

// Complete completes all the required options.
func (o *ControllerManagerOptions) Complete() error {
	allErrs := []error{}

	// complete leader election and client connection options
	if err := o.ControllerOptions.Complete(); err != nil {
		allErrs = append(allErrs, err)
	}
	return utilerrors.NewAggregate(allErrs)
}

// Validate validates all the required options.
func (o *ControllerManagerOptions) Validate() error {
	allErrs := []error{}

	// validate leader election and client connection options
	if err := o.ControllerOptions.Validate(); err != nil {
		allErrs = append(allErrs, err)
	}

	return utilerrors.NewAggregate(allErrs)
}

// Flags initializes flags by section name.
func (o *ControllerManagerOptions) Flags(allControllers, disabledByDefaultControllers []string) *cliflag.NamedFlagSets {
	fss := &cliflag.NamedFlagSets{}
	o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	o.DebuggingOptions.AddFlags(fss.FlagSet("profiling"))
	o.Metrics.AddFlags(fss.FlagSet("metrics"))
	// flags for leader election and client connection
	o.ControllerOptions.AddFlagSets(fss)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fss.FlagSet("feature gate"))
	miscfs := fss.FlagSet("misc")
	miscfs.BoolVar(&o.AnonymousAuthSupported, "anonymous-auth-supported", o.AnonymousAuthSupported, "Whether the anonymous access is allowed by the 'core' kubernetes server")
	miscfs.StringVar(&o.ReservedNamespace, "reserved-namespace", o.ReservedNamespace, "The default namespace to create Manifest in")
	miscfs.IntVar(&o.Threadiness, "threadiness", o.Threadiness, "The number of threads to use for controller workers")
	miscfs.StringVar(&o.ClusterAPIKubeconfig, "cluster-api-kubeconfig", o.ClusterAPIKubeconfig, "Path to a kubeconfig file pointing at the management cluster for cluster-api.")
	miscfs.StringSliceVar(&o.Controllers, "controllers", o.Controllers, fmt.Sprintf(""+
		"A list of controllers to enable. '*' enables all on-by-default controllers, 'foo' enables the controller "+
		"named 'foo', '-foo' disables the controller named 'foo'.\nAll controllers: %s\nDisabled-by-default controllers: %s",
		strings.Join(allControllers, ", "), strings.Join(disabledByDefaultControllers, ", ")))
	return fss
}
