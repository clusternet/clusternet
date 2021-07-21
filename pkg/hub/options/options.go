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

	"github.com/spf13/pflag"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	utilflowcontrol "k8s.io/apiserver/pkg/util/flowcontrol"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/klog/v2"

	clientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	clusternetopenapi "github.com/clusternet/clusternet/pkg/generated/openapi"
	"github.com/clusternet/clusternet/pkg/hub/apiserver"
)

const (
	openAPITitle = "Clusternet"
)

// HubServerOptions contains state for master/api server
type HubServerOptions struct {
	// No tunnel logging by default
	TunnelLogging bool

	RecommendedOptions *genericoptions.RecommendedOptions

	LoopbackSharedInformerFactory informers.SharedInformerFactory
}

// NewHubServerOptions returns a new HubServerOptions
func NewHubServerOptions() *HubServerOptions {
	o := &HubServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions("fake", nil),
	}
	return o
}

// Validate validates HubServerOptions
func (o *HubServerOptions) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.validateRecommendedOptions()...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *HubServerOptions) Complete() error {
	// TODO

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

func (o *HubServerOptions) AddFlags(fs *pflag.FlagSet) {
	o.addRecommendedOptionsFlags(fs)
}

func (o *HubServerOptions) addRecommendedOptionsFlags(fs *pflag.FlagSet) {
	// Copied from k8s.io/apiserver/pkg/server/options/recommended.go
	// and remove unused flags

	o.RecommendedOptions.SecureServing.AddFlags(fs)
	o.RecommendedOptions.Authentication.AddFlags(fs)
	o.RecommendedOptions.Authorization.AddFlags(fs)
	o.RecommendedOptions.Audit.LogOptions.AddFlags(fs)
	o.RecommendedOptions.Features.AddFlags(fs)
	o.RecommendedOptions.CoreAPI.AddFlags(fs)
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
	if err := o.RecommendedOptions.Features.ApplyTo(&config.Config); err != nil {
		return err
	}
	if err := o.RecommendedOptions.CoreAPI.ApplyTo(config); err != nil {
		return err
	}
	if initializers, err := o.RecommendedOptions.ExtraAdmissionInitializers(config); err != nil {
		return err
	} else if err := o.RecommendedOptions.Admission.ApplyTo(&config.Config, config.SharedInformerFactory, config.ClientConfig, o.RecommendedOptions.FeatureGate, initializers...); err != nil {
		return err
	}
	if utilfeature.DefaultFeatureGate.Enabled(features.APIPriorityAndFairness) {
		if config.ClientConfig != nil {
			config.FlowControl = utilflowcontrol.New(
				config.SharedInformerFactory,
				kubernetes.NewForConfigOrDie(config.ClientConfig).FlowcontrolV1beta1(),
				config.MaxRequestsInFlight+config.MaxMutatingRequestsInFlight,
				config.RequestTimeout/4,
			)
		} else {
			klog.Warningf("Neither kubeconfig is provided nor service-account is mounted, so APIPriorityAndFairness will be disabled")
		}
	}
	return nil
}
