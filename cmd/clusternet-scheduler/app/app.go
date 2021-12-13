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

	_ "github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/scheduler"
	"github.com/clusternet/clusternet/pkg/scheduler/options"
	"github.com/clusternet/clusternet/pkg/version"
)

var (
	// the command name
	cmdName = "clusternet-scheduler"
)

// NewSchedulerCommand creates a *cobra.Command object with default parameters
func NewSchedulerCommand(ctx context.Context) *cobra.Command {
	opts, err := options.NewSchedulerOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	cmd := &cobra.Command{
		Use:  cmdName,
		Long: `cluster-wise scheduler`,
		Run: func(cmd *cobra.Command, args []string) {
			if err = version.PrintAndExitIfRequested(cmdName); err != nil {
				klog.Exit(err)
			}

			if err = opts.Complete(); err != nil {
				klog.Exit(err)
			}
			if err = opts.Validate(args); err != nil {
				klog.Exit(err)
			}

			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				klog.V(1).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
			})

			// TODO: start metrics server

			sched, err := scheduler.NewScheduler(opts)
			if err != nil {
				klog.Exit(err)
			}
			if err = sched.Run(ctx); err != nil {
				klog.Exit(err)
			}
		},
	}

	flags := cmd.Flags()
	version.AddVersionFlag(flags)
	opts.AddFlags(flags)
	utilfeature.DefaultMutableFeatureGate.AddFlag(flags)

	return cmd
}
