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
	"os"

	"gopkg.in/yaml.v3"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/apis"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/utils"
)

// SchedulerOptions has all the params needed to run a Scheduler
type SchedulerOptions struct {
	*utils.ControllerOptions
	FrameworkOutOfTreeRegistry frameworkruntime.Registry
	SchedulerConfiguration     *apis.SchedulerConfiguration

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
		ControllerOptions: controllerOptions,
	}

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
	return utilerrors.NewAggregate(errors)
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
