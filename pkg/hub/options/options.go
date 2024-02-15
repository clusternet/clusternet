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
	"net"
	"net/http"
	"strings"
	"time"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/namespace/lifecycle"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"

	clientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	clusternetopenapi "github.com/clusternet/clusternet/pkg/generated/openapi"
	"github.com/clusternet/clusternet/pkg/hub/apiserver"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	openAPITitle = "Clusternet"
)

// HubServerOptions contains state for master/api server
type HubServerOptions struct {
	// No tunnel logging by default
	TunnelLogging bool

	// default namespace to create Manifest in
	// default to be "clusternet-reserved"
	ReservedNamespace string

	RecommendedOptions *genericoptions.RecommendedOptions

	LoopbackSharedInformerFactory informers.SharedInformerFactory

	*utils.ControllerOptions

	// advertise address to other peers
	PeerAdvertiseAddress net.IP
	// secure port used for communicating with peers
	PeerPort int
	// token used for authentication with peers
	PeerToken string

	// Flags hold the parsed CLI flags.
	Flags *cliflag.NamedFlagSets
}

// NewHubServerOptions returns a new HubServerOptions
func NewHubServerOptions() (*HubServerOptions, error) {
	controllerOpts, err := utils.NewControllerOptions("clusternet-hub", known.ClusternetSystemNamespace)
	if err != nil {
		return nil, err
	}
	controllerOpts.ClientConnection.QPS = rest.DefaultQPS * float32(10)
	controllerOpts.ClientConnection.Burst = int32(rest.DefaultBurst * 10)

	o := &HubServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions("fake", nil),
		ReservedNamespace:  known.ClusternetReservedNamespace,
		ControllerOptions:  controllerOpts,
		PeerPort:           8123,
		PeerToken:          "Cheugy",
	}
	o.initFlags()
	return o, nil
}

// Validate validates HubServerOptions
func (o *HubServerOptions) Validate() error {
	errors := []error{}
	errors = append(errors, o.validateRecommendedOptions()...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *HubServerOptions) Complete() error {
	o.RecommendedOptions.CoreAPI.CoreAPIKubeconfigPath = o.ControllerOptions.ClientConnection.Kubeconfig

	if o.PeerAdvertiseAddress == nil || o.PeerAdvertiseAddress.IsUnspecified() {
		hostIP, err := o.RecommendedOptions.SecureServing.DefaultExternalAddress()
		if err != nil {
			return fmt.Errorf("unable to find suitable network address: '%v'. "+
				"Try to set the PeerAdvertiseAddress directly or provide a valid BindAddress to fix this", err)
		}
		o.PeerAdvertiseAddress = hostIP
	}

	return nil
}

// Config returns config for the api server given HubServerOptions
func (o *HubServerOptions) Config() (*apiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	o.RecommendedOptions.ExtraAdmissionInitializers = func(c *genericapiserver.RecommendedConfig) ([]admission.PluginInitializer, error) {
		client, err := clientset.NewForConfig(c.LoopbackClientConfig)
		if err != nil {
			return nil, err
		}
		informerFactory := informers.NewSharedInformerFactory(client, c.LoopbackClientConfig.Timeout)
		o.LoopbackSharedInformerFactory = informerFactory
		// TODO: add initializer
		return []admission.PluginInitializer{}, nil
	}

	// remove NamespaceLifecycle admission plugin explicitly
	o.RecommendedOptions.Admission.DisablePlugins = append(o.RecommendedOptions.Admission.DisablePlugins, lifecycle.PluginName)

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)
	serverConfig.Config.RequestTimeout = time.Duration(40) * time.Second // override default 60s
	serverConfig.LongRunningFunc = func(r *http.Request, requestInfo *apirequest.RequestInfo) bool {
		if values := r.URL.Query()["watch"]; len(values) > 0 {
			switch strings.ToLower(values[0]) {
			case "true":
				return true
			default:
				return false
			}
		}
		return genericfilters.BasicLongRunningRequestCheck(sets.NewString("watch"), sets.NewString())(r, requestInfo)
	}
	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(clusternetopenapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = openAPITitle
	serverConfig.OpenAPIConfig.Info.Version = version.Get().GitVersion

	if err := o.recommendedOptionsApplyTo(serverConfig); err != nil {
		return nil, err
	}

	config := &apiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig:   apiserver.ExtraConfig{},
	}
	return config, nil
}

// initFlags initializes flags by section name.
func (o *HubServerOptions) initFlags() {
	if o.Flags != nil {
		return
	}

	fss := &cliflag.NamedFlagSets{}
	o.addRecommendedOptionsFlags(fss)

	// flags for clusternet-hub peer connections
	peerfs := fss.FlagSet("peer connections")
	peerfs.IPVar(&o.PeerAdvertiseAddress, "peer-advertise-address", o.PeerAdvertiseAddress, ""+
		"The IP address on which to advertise the clusternet-hub to other peers in the cluster. This "+
		"address must be reachable by the rest of the peers. If blank, the --bind-address "+
		"will be used. If --bind-address is unspecified, the host's default interface will "+
		"be used.")
	peerfs.IntVar(&o.PeerPort, "peer-port", o.PeerPort, "The port on which to serve HTTPS for communicating with peers.")
	peerfs.StringVar(&o.PeerToken, "peer-token", o.PeerToken, "The token for authentication with peers with peers.")

	// flags for leader election and client connection
	o.ControllerOptions.AddFlagSets(fss)

	miscfs := fss.FlagSet("misc")
	miscfs.BoolVar(&o.TunnelLogging, "enable-tunnel-logging", o.TunnelLogging, "Enable tunnel logging")
	miscfs.StringVar(&o.ReservedNamespace, "reserved-namespace", o.ReservedNamespace, "The default namespace to create Manifest in")

	utilfeature.DefaultMutableFeatureGate.AddFlag(fss.FlagSet("feature gate"))

	o.Flags = fss
}

func (o *HubServerOptions) addRecommendedOptionsFlags(nfs *cliflag.NamedFlagSets) {
	// Copied from k8s.io/apiserver/pkg/server/options/recommended.go
	// and remove unused flags

	o.RecommendedOptions.SecureServing.AddFlags(nfs.FlagSet("secure serving"))
	o.RecommendedOptions.Authentication.AddFlags(nfs.FlagSet("authentication"))
	o.RecommendedOptions.Authorization.AddFlags(nfs.FlagSet("authorization"))
	o.RecommendedOptions.Audit.LogOptions.AddFlags(nfs.FlagSet("audit"))
	o.RecommendedOptions.Features.AddFlags(nfs.FlagSet("profiling"))
	// flag "kubeconfig" has been declared in o.ControllerOptions
	//o.RecommendedOptions.CoreAPI.AddFlags(fs) // --kubeconfig flag
}

func (o *HubServerOptions) validateRecommendedOptions() []error {
	// Copied from k8s.io/apiserver/pkg/server/options/recommended.go
	// and remove unused Validate

	errors := []error{}
	errors = append(errors, o.RecommendedOptions.SecureServing.Validate()...)
	errors = append(errors, o.RecommendedOptions.Authentication.Validate()...)
	errors = append(errors, o.RecommendedOptions.Authorization.Validate()...)
	errors = append(errors, o.RecommendedOptions.Audit.LogOptions.Validate()...)
	errors = append(errors, o.RecommendedOptions.Features.Validate()...)
	errors = append(errors, o.RecommendedOptions.CoreAPI.Validate()...)
	return errors
}

func (o *HubServerOptions) recommendedOptionsApplyTo(config *genericapiserver.RecommendedConfig) error {
	// Copied from k8s.io/apiserver/pkg/server/options/recommended.go
	// and remove unused ApplyTo

	if err := o.RecommendedOptions.SecureServing.ApplyTo(&config.Config.SecureServing, &config.Config.LoopbackClientConfig); err != nil {
		return err
	}
	if err := o.RecommendedOptions.Authentication.ApplyTo(&config.Config.Authentication, config.SecureServing, config.OpenAPIConfig); err != nil {
		return err
	}
	if err := o.RecommendedOptions.Authorization.ApplyTo(&config.Config.Authorization); err != nil {
		return err
	}
	if err := o.RecommendedOptions.Audit.ApplyTo(&config.Config); err != nil {
		return err
	}

	kubeClient, err2 := kubernetes.NewForConfig(config.ClientConfig)
	if err2 != nil {
		return err2
	}

	if err := o.RecommendedOptions.Features.ApplyTo(&config.Config, kubeClient, config.SharedInformerFactory); err != nil {
		return err
	}
	if err := o.RecommendedOptions.CoreAPI.ApplyTo(config); err != nil {
		return err
	}

	initializers, err := o.RecommendedOptions.ExtraAdmissionInitializers(config)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(config.ClientConfig)
	if err != nil {
		return err
	}
	if err = o.RecommendedOptions.Admission.ApplyTo(
		&config.Config,
		config.SharedInformerFactory,
		kubeClient,
		dynamicClient,
		o.RecommendedOptions.FeatureGate,
		initializers...,
	); err != nil {
		return err
	}

	return nil
}
