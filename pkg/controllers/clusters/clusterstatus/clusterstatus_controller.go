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

package clusterstatus

import (
	"context"
	"net/http"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
)

// Controller is a controller that collects cluster status
type Controller struct {
	kubeClient       kubernetes.Interface
	lock             *sync.Mutex
	clusterStatus    *clusterapi.ManagedClusterStatus
	collectingPeriod metav1.Duration
	apiserverURL     string
	appPusherEnabled bool
	useSocket        bool
	parentAPIServer  string
}

func NewController(apiserverURL, parentAPIServerURL string, kubeClient kubernetes.Interface, collectingPeriod metav1.Duration) *Controller {
	return &Controller{
		kubeClient:       kubeClient,
		lock:             &sync.Mutex{},
		collectingPeriod: collectingPeriod,
		apiserverURL:     apiserverURL,
		appPusherEnabled: utilfeature.DefaultFeatureGate.Enabled(features.AppPusher),
		useSocket:        utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection),
		parentAPIServer:  parentAPIServerURL,
	}
}

func (c *Controller) Run(ctx context.Context) {
	wait.UntilWithContext(ctx, c.collectingClusterStatus, c.collectingPeriod.Duration)
}

func (c *Controller) collectingClusterStatus(ctx context.Context) {
	klog.V(7).Info("collecting cluster status...")
	clusterVersion, err := c.getKubernetesVersion(ctx)
	if err != nil {
		klog.Warningf("failed to collect kubernetes version: %v", err)
		return
	}

	clusterCIDR, err := c.discoverClusterCIDR()
	if err != nil {
		klog.Warningf("failed to discover cluster CIDR: %v", err)
		return
	}

	serviceCIDR, err := c.discoverServiceCIDR()
	if err != nil {
		klog.Warningf("failed to discover service CIDR: %v", err)
		return
	}

	var status clusterapi.ManagedClusterStatus
	status.KubernetesVersion = clusterVersion.GitVersion
	status.Platform = clusterVersion.Platform
	status.APIServerURL = c.apiserverURL
	status.Healthz = c.getHealthStatus(ctx, "/healthz")
	status.Livez = c.getHealthStatus(ctx, "/livez")
	status.Readyz = c.getHealthStatus(ctx, "/readyz")
	status.AppPusher = c.appPusherEnabled
	status.UseSocket = c.useSocket
	status.ParentAPIServerURL = c.parentAPIServer
	status.ClusterCIDR = clusterCIDR
	status.ServiceCIDR = serviceCIDR
	c.setClusterStatus(status)
}

func (c *Controller) setClusterStatus(status clusterapi.ManagedClusterStatus) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.clusterStatus == nil {
		c.clusterStatus = new(clusterapi.ManagedClusterStatus)
	}

	c.clusterStatus = &status
	c.clusterStatus.LastObservedTime = metav1.Now()
	klog.V(7).Infof("current cluster status is %#v", status)
}

func (c *Controller) GetClusterStatus() *clusterapi.ManagedClusterStatus {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.clusterStatus == nil {
		return nil
	}

	return c.clusterStatus.DeepCopy()
}

func (c *Controller) getKubernetesVersion(ctx context.Context) (*version.Info, error) {
	return c.kubeClient.Discovery().ServerVersion()
}

func (c *Controller) getHealthStatus(ctx context.Context, path string) bool {
	var statusCode int
	c.kubeClient.Discovery().RESTClient().Get().AbsPath(path).Do(ctx).StatusCode(&statusCode)
	return statusCode == http.StatusOK
}

// discoverServiceCIDR returns the service CIDR for the cluster.
func (c *Controller)discoverServiceCIDR() (string, error) {
	return findPodIPRange(c.nodeLister, c.podLister)
}

// discoverClusterCIDR returns the cluster CIDR for the cluster.
func (c *Controller)discoverClusterCIDR() (string, error) {
	return findClusterIPRange(c.podLister)
}