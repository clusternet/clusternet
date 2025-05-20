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
	"os"

	"gopkg.in/yaml.v3"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/metrics"
	controllermanageroptions "k8s.io/controller-manager/options"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/apis"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// DefaultClusternetSchedulerPort is the default port for the scheduler status server.
	// May be overridden by a flag at startup.
	DefaultClusternetSchedulerPort = 10659
)

// SchedulerOptions has all the params needed to run a Scheduler
type SchedulerOptions struct {
	SecureServing *apiserveroptions.SecureServingOptionsWithLoopback

	// DebuggingOptions holds the Debugging options.
	DebuggingOptions *controllermanageroptions.DebuggingOptions

	*utils.ControllerOptions
	FrameworkOutOfTreeRegistry frameworkruntime.Registry
	SchedulerConfiguration     *apis.SchedulerConfiguration
	Metrics                    *metrics.Options

	// ConfigFile is the location of the scheduler server's configuration file.
	ConfigFile string

	// Flags hold the parsed CLI flags.
	Flags *cliflag.NamedFlagSets
}

// NewSchedulerOptions returns a new SchedulerOptions
func NewSchedulerOptions() (*SchedulerOptions, error) {
	controllerOptions, err := utils.NewControllerOptions("clusternet-scheduler", known.ClusternetSystemNamespace)
	if err != nil {
		return nil, err
	}

	o := &SchedulerOptions{
		SecureServing:     apiserveroptions.NewSecureServingOptions().WithLoopback(),
		DebuggingOptions:  controllermanageroptions.RecommendedDebuggingOptions(),
		ControllerOptions: controllerOptions,
		Metrics:           metrics.NewOptions(),
	}

	// Set the PairName but leave certificate directory blank to generate in-memory by default
	o.SecureServing.ServerCert.CertDirectory = ""
	o.SecureServing.ServerCert.PairName = "clusternet-scheduler"
	o.SecureServing.BindPort = DefaultClusternetSchedulerPort

	o.initFlags()
	return o, nil
}

// Validate validates SchedulerOptions
func (o *SchedulerOptions) Validate() error {
	errors := []error{}
	// validate leader election and client connection options
	if err := o.ControllerOptions.Validate(); err != nil {
		errors = append(errors, err)
	}
	if o.SchedulerConfiguration != nil {
		if err := apis.ValidateSchedulerConfiguration(o.SchedulerConfiguration); err != nil {
			errors = append(errors, err)
		}
	}
	errors = append(errors, o.SecureServing.Validate()...)
	errors = append(errors, o.DebuggingOptions.Validate()...)
	errors = append(errors, o.Metrics.Validate()...)
	return utilerrors.NewAggregate(errors)
}

func (o *SchedulerOptions) Config() error {
	if o.SecureServing != nil {
		if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
			return fmt.Errorf("error creating self-signed certificates: %v", err)
		}
	}

	o.Metrics.Apply()
	return nil
}

// Complete fills in fields required to have valid data
func (o *SchedulerOptions) Complete() error {
	allErrs := []error{}

	// complete leader election and client connection options
	if err := o.ControllerOptions.Complete(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(o.ConfigFile) > 0 {
		schedulerConfiguration, err := loadConfigFromFile(o.ConfigFile)
		if err != nil {
			allErrs = append(allErrs, err)
		} else {
			o.SchedulerConfiguration = schedulerConfiguration
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

// initFlags initializes flags by section name.
func (o *SchedulerOptions) initFlags() {
	if o.Flags != nil {
		return
	}

	fss := &cliflag.NamedFlagSets{}
	fs := fss.FlagSet("misc")
	fs.StringVar(&o.ConfigFile, "config", o.ConfigFile, "The path to the configuration file.")

	o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	o.DebuggingOptions.AddFlags(fss.FlagSet("profiling"))
	o.Metrics.AddFlags(fss.FlagSet("metrics"))
	o.ControllerOptions.AddFlagSets(fss)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fss.FlagSet("feature gate"))
	o.Flags = fss
}

func loadConfigFromFile(file string) (*apis.SchedulerConfiguration, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	configObj := &apis.SchedulerConfiguration{}
	err = yaml.Unmarshal(data, configObj)
	if err != nil {
		return nil, err
	}
	apis.SetDefaultsSchedulerConfiguration(configObj)
	return configObj, nil
}
