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
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	componentbaseconfig "k8s.io/component-base/config"
	componentbaseoptions "k8s.io/component-base/config/options"
	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/known"
)

// SchedulerOptions has all the params needed to run a Scheduler
type SchedulerOptions struct {
	// LeaderElection defines the configuration of leader election client.
	LeaderElection componentbaseconfig.LeaderElectionConfiguration

	// ClientConnection specifies the kubeconfig file and client connection
	// settings for the proxy server to use when communicating with the apiserver.
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
}

// NewSchedulerOptions returns a new SchedulerOptions
func NewSchedulerOptions() (*SchedulerOptions, error) {
	versionedClientConnection := componentbaseconfigv1alpha1.ClientConnectionConfiguration{}
	versionedLeaderElection := componentbaseconfigv1alpha1.LeaderElectionConfiguration{
		ResourceLock:      "lease", // Use lease-based leader election to reduce cost.
		ResourceName:      "clusternet-scheduler",
		ResourceNamespace: known.ClusternetSystemNamespace,
	}
	// Use the default ClientConnectionConfiguration and LeaderElectionConfiguration options
	componentbaseconfigv1alpha1.RecommendedDefaultClientConnectionConfiguration(&versionedClientConnection)
	componentbaseconfigv1alpha1.RecommendedDefaultLeaderElectionConfiguration(&versionedLeaderElection)

	o := &SchedulerOptions{
		ClientConnection: componentbaseconfig.ClientConnectionConfiguration{},
		LeaderElection:   componentbaseconfig.LeaderElectionConfiguration{},
	}

	schedScheme := runtime.NewScheme()
	utilruntime.Must(componentbaseconfigv1alpha1.AddToScheme(schedScheme))
	if err := schedScheme.Convert(&versionedClientConnection, &o.ClientConnection, nil); err != nil {
		return nil, err
	}
	if err := schedScheme.Convert(&versionedLeaderElection, &o.LeaderElection, nil); err != nil {
		return nil, err
	}

	return o, nil
}

// Validate validates SchedulerOptions
func (o *SchedulerOptions) Validate(args []string) error {
	errors := []error{}
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *SchedulerOptions) Complete() error {
	// TODO

	return nil
}

// AddFlags adds flags for SchedulerOptions.
func (o *SchedulerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClientConnection.Kubeconfig, "kubeconfig", o.ClientConnection.Kubeconfig, "Path to a kubeconfig file pointing at the 'core' kubernetes server. Only required if out-of-cluster.")
	fs.Float32Var(&o.ClientConnection.QPS, "kube-api-qps", o.ClientConnection.QPS, "QPS to use while talking with the 'core' kubernetes apiserver.")
	fs.Int32Var(&o.ClientConnection.Burst, "kube-api-burst", o.ClientConnection.Burst, "Burst to use while talking with 'core' kubernetes apiserver.")

	componentbaseoptions.BindLeaderElectionFlags(&o.LeaderElection, fs)
	if err := fs.MarkHidden("leader-elect-resource-lock"); err != nil {
		klog.Errorf("failed to set a flag to hidden: %v", err)
	}
}
