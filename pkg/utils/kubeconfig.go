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
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apiserver/pkg/authentication/user"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	proxiesapi "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
)

// createBasicKubeConfig creates a basic, general KubeConfig object that then can be extended
func createBasicKubeConfig(serverURL, clusterName, userName string, caCert []byte) *clientcmdapi.Config {
	// Use the cluster and the username as the context name
	contextName := fmt.Sprintf("%s@%s", userName, clusterName)

	var insecureSkipTLSVerify bool
	if caCert == nil {
		insecureSkipTLSVerify = true
	}

	return &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			clusterName: {
				Server:                   serverURL,
				InsecureSkipTLSVerify:    insecureSkipTLSVerify,
				CertificateAuthorityData: caCert,
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
func CreateKubeConfigWithToken(serverURL, token string, caCert []byte) *clientcmdapi.Config {
	userName := "clusternet"
	clusterName := "clusternet-cluster"
	config := createBasicKubeConfig(serverURL, clusterName, userName, caCert)
	config.AuthInfos[userName] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	return config
}

// CreateKubeConfigForSocketProxyWithToken creates a KubeConfig object with access to the API server with a token
func CreateKubeConfigForSocketProxyWithToken(serverURL, token string) *clientcmdapi.Config {
	userName := "clusternet"
	clusterName := "clusternet-cluster"
	config := createBasicKubeConfig(serverURL, clusterName, userName, nil)
	config.AuthInfos[userName] = &clientcmdapi.AuthInfo{
		Username:    user.Anonymous,
		Impersonate: "clusternet",
		ImpersonateUserExtra: map[string][]string{
			"clusternet-token": {
				token,
			},
		},
	}
	return config
}

// LoadsKubeConfig tries to load kubeconfig from specified kubeconfig file or in-cluster config
func LoadsKubeConfig(kubeConfigPath string, flowRate int) (*rest.Config, error) {
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
	return applyDefaultRateLimiter(config, flowRate), nil
}

// GenerateKubeConfigFromToken composes a kubeconfig from token
func GenerateKubeConfigFromToken(serverURL, token string, caCert []byte, flowRate int) (*rest.Config, error) {
	clientConfig := CreateKubeConfigWithToken(serverURL, token, caCert)
	config, err := clientcmd.NewDefaultClientConfig(*clientConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error while creating kubeconfig: %v", err)
	}

	return applyDefaultRateLimiter(config, flowRate), nil
}

func applyDefaultRateLimiter(config *rest.Config, flowRate int) *rest.Config {
	if flowRate < 0 {
		flowRate = 1
	}

	// here we magnify the default qps and burst in client-go
	config.QPS = rest.DefaultQPS * float32(flowRate)
	config.Burst = rest.DefaultBurst * flowRate

	return config
}

func GetChildClusterConfig(secretLister corev1lister.SecretLister, clusterLister clusterlisters.ManagedClusterLister,
	namespace, clusterID, parentAPIServerURL string) (*clientcmdapi.Config, error) {
	childClusterSecret, err := secretLister.Secrets(namespace).Get(known.ChildClusterSecretName)
	if err != nil {
		return nil, err
	}

	labelSet := labels.Set{}
	if len(clusterID) > 0 {
		labelSet[known.ClusterIDLabel] = clusterID
	}

	mcls, err := clusterLister.ManagedClusters(namespace).List(
		labels.SelectorFromSet(labelSet))
	if err != nil {
		return nil, err
	}
	if mcls == nil {
		return nil, fmt.Errorf("failed to find a ManagedCluster declaration in namespace %s", namespace)
	}

	var config *clientcmdapi.Config
	if len(mcls) > 1 {
		klog.Warningf("found multiple ManagedCluster declarations in namespace %s", namespace)
	}
	if mcls[0].Status.UseSocket {
		childClusterAPIServer, err := getChildAPIServerProxyURL(parentAPIServerURL, mcls[0])
		if err != nil {
			return nil, err
		}
		config = CreateKubeConfigForSocketProxyWithToken(
			childClusterAPIServer,
			string(childClusterSecret.Data[corev1.ServiceAccountTokenKey]),
		)
	} else {
		config = CreateKubeConfigWithToken(
			string(childClusterSecret.Data[known.ClusterAPIServerURLKey]),
			string(childClusterSecret.Data[corev1.ServiceAccountTokenKey]),
			childClusterSecret.Data[corev1.ServiceAccountRootCAKey],
		)
	}
	return config, nil
}

func getChildAPIServerProxyURL(parentAPIServerURL string, mcls *clusterapi.ManagedCluster) (string, error) {
	if mcls == nil {
		return "", errors.New("unable to generate child cluster apiserver proxy url from nil ManagedCluster object")
	}

	if len(parentAPIServerURL) == 0 {
		return "", errors.New("got empty parent apiserver url")
	}

	return strings.Join([]string{
		strings.TrimRight(parentAPIServerURL, "/"),
		"apis", proxiesapi.SchemeGroupVersion.String(), "sockets", string(mcls.Spec.ClusterID),
		"proxy/direct"}, "/"), nil
}
