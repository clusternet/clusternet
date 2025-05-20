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

package context

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	genericcontrollermanager "k8s.io/controller-manager/app"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"
	mcsclientset "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsinformers "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions"

	"github.com/clusternet/clusternet/pkg/controllermanager/options"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
)

// ControllerContext defines the context object for controller.
type ControllerContext struct {
	Opts                      *options.ControllerManagerOptions
	KubeConfig                *rest.Config
	KubeClient                *kubernetes.Clientset
	ElectionClient            *kubernetes.Clientset
	ClusternetClient          *clusternet.Clientset
	McsClient                 *mcsclientset.Clientset
	ClusternetInformerFactory informers.SharedInformerFactory
	KubeInformerFactory       kubeinformers.SharedInformerFactory
	McsInformerFactory        mcsinformers.SharedInformerFactory
	ClientBuilder             clientbuilder.ControllerClientBuilder
	EventRecorder             record.EventRecorder

	// InformersStarted is closed after all of the controllers have been initialized and are running.  After this point it is safe,
	// for an individual controller to start the shared informers. Before it is closed, they should not.
	InformersStarted chan struct{}
}

// StartControllers starts a set of controllers with a specified ControllerContext
func (c *ControllerContext) StartControllers(ctx context.Context, initializers Initializers, controllersDisabledByDefault sets.String) error {
	for controllerName, initFn := range initializers {
		if !c.IsControllerEnabled(controllerName, controllersDisabledByDefault) {
			klog.Warningf("controller %q is disabled", controllerName)
			continue
		}
		klog.V(1).Infof("Starting controller %q", controllerName)
		started, err := initFn(c, ctx)
		if err != nil {
			klog.Errorf("Error starting controller %q", controllerName)
			return err
		}
		if !started {
			klog.Warningf("Skipping controller %q", controllerName)
			continue
		}
		klog.Infof("Started controller %q", controllerName)
	}
	return nil
}

func (c *ControllerContext) StartShardInformerFactories(ctx context.Context) {
	c.KubeInformerFactory.Start(ctx.Done())
	c.ClusternetInformerFactory.Start(ctx.Done())
	c.McsInformerFactory.Start(ctx.Done())
}

// IsControllerEnabled check if a specified controller enabled or not.
func (c ControllerContext) IsControllerEnabled(name string, controllersDisabledByDefault sets.String) bool {
	return genericcontrollermanager.IsControllerEnabled(name, controllersDisabledByDefault, c.Opts.Controllers)
}

// InitFunc is used to launch a particular controller.
// Any error returned will cause the controller process to `Fatal`
// The bool indicates whether the controller was enabled.
type InitFunc func(controllerContext *ControllerContext, ctx context.Context) (enabled bool, err error)

// Initializers is a public map of named controller groups
type Initializers map[string]InitFunc

// KnownControllerNames returns all known controller names
func (i Initializers) KnownControllerNames() []string {
	return sets.StringKeySet(i).List()
}
