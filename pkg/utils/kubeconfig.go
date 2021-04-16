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

package utils

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// CreateBasicKubeConfig creates a basic, general KubeConfig object that then can be extended
func CreateBasicKubeConfig(serverURL, clusterName, userName string, insecureSkipTLSVerify bool) *clientcmdapi.Config {
	// Use the cluster and the username as the context name
	contextName := fmt.Sprintf("%s@%s", userName, clusterName)

	return &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			clusterName: {
				Server:                serverURL,
				InsecureSkipTLSVerify: insecureSkipTLSVerify,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			contextName: {
				Cluster:  clusterName,
				AuthInfo: userName,
			},
		},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{},
		CurrentContext: contextName,
	}
}

// CreateKubeConfigWithToken creates a KubeConfig object with access to the API server with a token
func CreateKubeConfigWithToken(serverURL string, token string, insecureSkipTLSVerify bool) *clientcmdapi.Config {
	userName := "userName"
	clusterName := "clusterName"
	config := CreateBasicKubeConfig(serverURL, clusterName, userName, insecureSkipTLSVerify)
	config.AuthInfos[userName] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	return config
}

// GetKubeConfig loads kubeconfig
func GetKubeConfig(kubeConfigPath string, flowRate int) (*rest.Config, error) {
	if len(kubeConfigPath) == 0 {
		// use in-cluster config
		return rest.InClusterConfig()
	}

	clientConfig, err := clientcmd.LoadFromFile(kubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error while loading kubeconfig from file %v: %v", kubeConfigPath, err)
	}
	config, err := clientcmd.NewDefaultClientConfig(*clientConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error while creating kubeconfig: %v", err)
	}

	if flowRate < 0 {
		flowRate = 1
	}

	// here we magnify the default qps and burst
	config.QPS = rest.DefaultQPS * float32(flowRate)
	config.Burst = rest.DefaultBurst * flowRate

	return config, nil
}
