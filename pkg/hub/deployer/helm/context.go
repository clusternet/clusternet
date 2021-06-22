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

package helm

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type deployContext struct {
	clientConfig             clientcmd.ClientConfig
	restConfig               *rest.Config
	cachedDiscoveryInterface discovery.CachedDiscoveryInterface
	restMapper               meta.RESTMapper
}

func newDeployContext(config *clientcmdapi.Config) (*deployContext, error) {
	clientConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{})
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error while creating deployContext: %v", err)
	}
	restConfig.QPS = 5
	restConfig.Burst = 10

	kubeclient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("error while creating deployContext: %v", err)
	}

	discoveryClient := cacheddiscovery.NewMemCacheClient(kubeclient.Discovery())
	discoveryRESTMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	dctx := &deployContext{
		clientConfig:             clientConfig,
		restConfig:               restConfig,
		cachedDiscoveryInterface: discoveryClient,
		restMapper:               discoveryRESTMapper,
	}

	return dctx, nil
}

func (dctx *deployContext) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return dctx.clientConfig
}

func (dctx *deployContext) ToRESTConfig() (*rest.Config, error) {
	return dctx.restConfig, nil
}

func (dctx *deployContext) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return dctx.cachedDiscoveryInterface, nil
}

func (dctx *deployContext) ToRESTMapper() (meta.RESTMapper, error) {
	return dctx.restMapper, nil
}
