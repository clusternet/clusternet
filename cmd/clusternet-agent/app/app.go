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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/agent"
	"github.com/clusternet/clusternet/pkg/agent/options"
	_ "github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/version"
)

func init() {
	utilruntime.Must(logsapi.AddFeatureGates(utilfeature.DefaultMutableFeatureGate))
}

var (
	// the command name
	cmdName = "clusternet-agent"
)

// NewClusternetAgentCmd creates a *cobra.Command object with default parameters
func NewClusternetAgentCmd(ctx context.Context) *cobra.Command {
	opts, err := options.NewOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	cmd := &cobra.Command{
		Use:  cmdName,
		Long: `Running in child cluster, responsible for cluster registration, tunnel setup, cluster heartbeat, etc`,
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if len(arg) > 0 {
					return fmt.Errorf("%q does not take any arguments, got %q", cmd.CommandPath(), args)
				}
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			if err = version.PrintAndExitIfRequested(cmdName); err != nil {
				klog.Exit(err)
			}

			// Activate logging as soon as possible, after that
			// show flags with the final logging configuration.
			if err = logsapi.ValidateAndApply(opts.Logs, utilfeature.DefaultFeatureGate); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			cliflag.PrintFlags(cmd.Flags())

			if err = opts.Complete(); err != nil {
				klog.Exit(err)
			}
			if err = opts.Validate(); err != nil {
				klog.Exit(err)
			}

			agent, err2 := agent.NewAgent(opts)
			if err2 != nil {
				klog.Exit(err2)
			}
			if err2 = agent.Run(ctx); err2 != nil {
				klog.Exit(err2)
			}

		},
	}

	nfs := opts.Flags
	version.AddVersionFlag(nfs.FlagSet("global"))
	globalflag.AddGlobalFlags(nfs.FlagSet("global"), cmd.Name(), logs.SkipLoggingConfigurationFlags())
	fs := cmd.Flags()
	for _, f := range nfs.FlagSets {
		fs.AddFlagSet(f)
	}
	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, *nfs, cols)

	return cmd
}
