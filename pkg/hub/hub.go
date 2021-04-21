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

package hub

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/hub/approver"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// default resync time
	DefaultResync = time.Minute * 30
	// default number of threads
	DefaultThreadiness = 2
)

// Hub defines configuration for clusternet-hub
type Hub struct {
	ctx context.Context

	CRRApprover               *approver.CRRApprover
	clusternetInformerFactory informers.SharedInformerFactory
	kubeclient                *kubernetes.Clientset
	clusternetclient          *clusternetClientSet.Clientset
}

// NewHub returns a new Hub.
func NewHub(ctx context.Context, kubeConfig string) (*Hub, error) {
	config, err := utils.LoadsKubeConfig(kubeConfig, 10)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	kubeclient := kubernetes.NewForConfigOrDie(config)
	clusternetclient := clusternetClientSet.NewForConfigOrDie(config)

	// creates the informer factory
	clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetclient, DefaultResync)
	approver, err := approver.NewCRRApprover(ctx, kubeclient, clusternetclient, clusternetInformerFactory.Clusters().V1beta1().ClusterRegistrationRequests())
	if err != nil {
		return nil, err
	}

	hub := &Hub{
		CRRApprover:               approver,
		ctx:                       ctx,
		kubeclient:                kubeclient,
		clusternetclient:          clusternetclient,
		clusternetInformerFactory: clusternetInformerFactory,
	}

	// Start the informer factories to begin populating the informer caches
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	clusternetInformerFactory.Start(ctx.Done())

	return hub, nil
}

func (hub *Hub) Run() error {
	klog.Info("starting Clusternet Hub ...")

	// TODO: goroutine
	hub.CRRApprover.Run(DefaultThreadiness)
	return nil
}
