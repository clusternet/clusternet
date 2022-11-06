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

package app

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/agent"
	_ "github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/version"
)

var (
	// the command name
	cmdName = "clusternet-agent"
)

// NewClusternetAgentCmd creates a *cobra.Command object with default parameters
func NewClusternetAgentCmd(ctx context.Context) *cobra.Command {
	opts, err := NewOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	cmd := &cobra.Command{
		Use:  cmdName,
		Long: `Running in child cluster, responsible for cluster registration, tunnel setup, cluster heartbeat, etc`,
		Run: func(cmd *cobra.Command, args []string) {
			if err2 := version.PrintAndExitIfRequested(cmdName); err2 != nil {
				klog.Exit(err2)
			}

			if err2 := opts.Complete(); err2 != nil {
				klog.Exit(err2)
			}
			if err2 := opts.Validate(); err2 != nil {
				klog.Exit(err2)
			}

			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				klog.V(1).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
			})

			agent, err2 := agent.NewAgent(opts.clusterRegistration, opts.ControllerOptions)
			if err2 != nil {
				klog.Exit(err2)
			}
			if err2 = agent.Run(ctx); err2 != nil {
				klog.Exit(err2)
			}

		},
	}

	version.AddVersionFlag(cmd.Flags())
	opts.AddFlags(cmd.Flags())
	utilfeature.DefaultMutableFeatureGate.AddFlag(cmd.Flags())

	return cmd
}
