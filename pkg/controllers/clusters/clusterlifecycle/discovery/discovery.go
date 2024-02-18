/*
Copyright 2022 The Clusternet Authors.

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

package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/dixudx/yacht"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	capiv1alpha4 "sigs.k8s.io/cluster-api/api/v1alpha4"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiaddonsv1beta1 "sigs.k8s.io/cluster-api/exp/addons/api/v1beta1"

	"github.com/clusternet/clusternet/pkg/known"
)

const (
	clusterResource = "clusters"

	clusterAPIClusternetNamespace = "capi-system"

	clusternetClusterAPI           = "clusternet-cluster-api"
	clusternetAgentDeployConfigmap = "clusternet-agent-deploy"
	clusternetAgentRBACConfigmap   = "clusternet-agent-rbac"
)

var (
	v1alpha4CAPIGVR = schema.GroupVersionResource{
		Group:    capiv1alpha4.GroupVersion.Group,
		Version:  capiv1alpha4.GroupVersion.Version,
		Resource: clusterResource,
	}

	v1beta1CAPIGVR = schema.GroupVersionResource{
		Group:    capiv1beta1.GroupVersion.Group,
		Version:  capiv1beta1.GroupVersion.Version,
		Resource: clusterResource,
	}
)

// Controller is a controller that discovers clusters provisioned with cluster-api.
// Please make sure that feature gate ClusterResourceSet is enabled in capi-controller-manager running in
// cluster-api management cluster.
// This controller only interacts with cluster-api management cluster
type Controller struct {
	kubeClient          kubernetes.Interface
	clusterapiClient    dynamic.Interface
	capiInformerFactory dynamicinformer.DynamicSharedInformerFactory
	capiGVR             schema.GroupVersionResource
}

// NewController creates a new Controller
func NewController(clusterapiClient dynamic.Interface, kubeClient kubernetes.Interface, defaultResync time.Duration) *Controller {
	return &Controller{
		clusterapiClient:    clusterapiClient,
		kubeClient:          kubeClient,
		capiInformerFactory: dynamicinformer.NewDynamicSharedInformerFactory(clusterapiClient, defaultResync),
		capiGVR:             discoveryClusterGVR(clusterapiClient),
	}
}

func (c *Controller) Run(ctx context.Context) {
	klog.Warning("please make sure feature gate ClusterResourceSet is enabled in capi-controller-manager " +
		"running in cluster-api management cluster")

	var err error
	var regCfg *RegistrationConfig
	initCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.UntilWithContext(initCtx, func(ctx context.Context) {
		klog.V(4).Infof("fetching registration config from cluster-api management cluster ...")
		// get registration config
		regCfg, err = fetchRegistrationConfig(ctx, c.kubeClient.CoreV1().Secrets(clusterAPIClusternetNamespace))
		if err == nil {
			// success
			cancel()
		}
	}, 0)
	klog.V(4).Infof("successfully fetching registration config from cluster-api management cluster")

	// create a yacht controller for cluster discovery
	controller := yacht.NewController("cluster-discovery").
		WithWorkers(2).
		WithHandlerFunc(func(key interface{}) (requeueAfter *time.Duration, err error) {
			// Convert the namespace/name string into a distinct namespace and name
			ns, name, err := cache.SplitMetaNamespaceKey(key.(string))
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
				return nil, err
			}

			clusterObj, err := c.capiInformerFactory.ForResource(c.capiGVR).Lister().ByNamespace(ns).Get(name)
			if err != nil {
				return nil, err
			}
			cluster, ok := clusterObj.(*unstructured.Unstructured)
			if !ok {
				return nil, nil
			}

			if !clusterIsReady(cluster) {
				return nil, fmt.Errorf("cluster %s is not ready yet. will retry later", key)
			}
			klog.V(4).Infof("cluster %s is ready for registration to clusternet control plane", key)
			timeOutCtx, tCancel := context.WithTimeout(ctx, time.Minute*1)
			defer tCancel()
			err = registerCluster(timeOutCtx, cluster, regCfg, c.capiGVR.Version, c.kubeClient, c.clusterapiClient)
			if err == nil {
				klog.V(4).Infof("successfully registering cluster %s to clusternet control plane, and it may take a while to finish the whole process", key)
			}
			return nil, err
		}).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// here we want to filter out the resource
			old, ok := oldObj.(*unstructured.Unstructured)
			if !ok {
				return false, nil
			}

			// un-register in-deleting clusters
			if _, isDeleting := clusterIsDeleting(old); isDeleting {
				// TODO
				//go func() {}()
				return false, nil
			}

			return true, nil
		})

	clusterInformer := c.capiInformerFactory.ForResource(c.capiGVR).Informer()
	_, err = clusterInformer.AddEventHandler(controller.DefaultResourceEventHandlerFuncs())
	if err != nil {
		klog.Fatalf("failed to add event handler for cluster discovery: %v", err)
		return
	}

	// start the informer factory and run the controller
	c.capiInformerFactory.Start(ctx.Done())
	controller.WithCacheSynced(clusterInformer.HasSynced).Run(ctx)
}

// discoveryClusterGVR discovers the serving api version of "Cluster" in "cluster.x-k8s.io".
func discoveryClusterGVR(clusterapiClient dynamic.Interface) schema.GroupVersionResource {
	// first use cluster.x-k8s.io/v1beta1
	_, err := clusterapiClient.Resource(v1beta1CAPIGVR).List(context.TODO(), metav1.ListOptions{})
	if err == nil {
		return v1beta1CAPIGVR
	}

	// fall back to use cluster.x-k8s.io/v1alpha4
	return v1alpha4CAPIGVR
}

// clusterIsDeleting checks whether the cluster is in deleting
func clusterIsDeleting(obj *unstructured.Unstructured) (*ClusterMetadata, bool) {
	clusterMetadata := getClusterMetadata(obj)
	if clusterMetadata == nil {
		return nil, false
	}
	return clusterMetadata, clusterMetadata.DeletionTimestamp != nil
}

// clusterIsReady checks whether the cluster is ready
func clusterIsReady(obj *unstructured.Unstructured) bool {
	clusterMetadata := getClusterMetadata(obj)
	if clusterMetadata == nil {
		return false
	}
	return clusterMetadata.Phase == string(capiv1beta1.ClusterPhaseProvisioned)
}

func registerCluster(ctx context.Context,
	cluster *unstructured.Unstructured,
	regCfg *RegistrationConfig,
	capiVersion string,
	kubeClient kubernetes.Interface,
	capiClient dynamic.Interface,
) error {
	// create configmap for clusternet-agent deployment
	err := createConfigmap(ctx, clusternetAgentDeployConfigmap, ClusternetAgentDeployment, struct {
		Namespace string
		Image     string
		Token     string
		ParentURL string
	}{
		Namespace: known.ClusternetSystemNamespace,
		Image:     string(regCfg.Image),
		Token:     string(regCfg.RegistrationToken),
		ParentURL: string(regCfg.ParentURL),
	}, kubeClient.CoreV1().ConfigMaps(cluster.GetNamespace()))
	if err != nil {
		return err
	}

	// create configmap for clusternet-agent rbac
	err = createConfigmap(ctx, clusternetAgentRBACConfigmap, ClusternetAgentRBACRules, struct {
		Namespace string
	}{
		Namespace: known.ClusternetSystemNamespace,
	}, kubeClient.CoreV1().ConfigMaps(cluster.GetNamespace()))
	if err != nil {
		return err
	}

	return createClusterResourceSet(ctx, cluster.GetNamespace(), capiVersion, capiClient)
}

func createClusterResourceSet(ctx context.Context, namespace, capiVersion string, capiClient dynamic.Interface) error {
	clusterRS := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": schema.GroupVersion{
				Version: capiVersion,
				Group:   capiaddonsv1beta1.GroupVersion.Group}.String(),
			"kind": "ClusterResourceSet",
			"metadata": map[string]interface{}{
				"name": clusternetClusterAPI,
				//"namespace":  clusterAPIClusternetNamespace,
				"labels": map[string]interface{}{
					known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
				},
			},
			"spec": map[string]interface{}{
				"clusterSelector": map[string]interface{}{
					"matchExpressions": []interface{}{
						map[string]interface{}{
							"key":      capiv1beta1.ClusterNameLabel,
							"operator": metav1.LabelSelectorOpExists,
						},
					},
				},
				"resources": []interface{}{
					map[string]interface{}{
						"name": clusternetAgentDeployConfigmap,
						"kind": "ConfigMap",
					},
					map[string]interface{}{
						"name": clusternetAgentRBACConfigmap,
						"kind": "ConfigMap",
					},
				},
			},
		},
	}

	createCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntilWithContext(createCtx, func(ctx context.Context) {
		_, err := capiClient.Resource(schema.GroupVersionResource{
			Group:    capiaddonsv1beta1.GroupVersion.Group,
			Version:  capiVersion,
			Resource: "clusterresourcesets",
		}).Namespace(namespace).Create(ctx, clusterRS, metav1.CreateOptions{})
		if err == nil || apierrors.IsAlreadyExists(err) {
			cancel()
			return
		}
		klog.ErrorDepth(2, fmt.Sprintf("failed to create clusterresourceset %s/%s to cluster-api manament cluster: %v", namespace, clusternetClusterAPI, err))
	}, known.DefaultRetryPeriod, 0.3, true)
	return nil
}

func createConfigmap(ctx context.Context, tmplName, tmplStr string, obj interface{}, cmInterface corev1.ConfigMapInterface) error {
	resource, err := parseManifest(tmplName, tmplStr, obj)
	if err != nil {
		return err
	}

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: tmplName,
		},
		Data: map[string]string{
			tmplName: resource,
		},
	}

	createCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntilWithContext(createCtx, func(ctx context.Context) {
		_, err2 := cmInterface.Create(ctx, cm, metav1.CreateOptions{})
		if err2 == nil {
			cancel()
			return
		}

		if !apierrors.IsAlreadyExists(err2) {
			klog.ErrorDepth(2, fmt.Sprintf("failed to create secret %s to cluster-api manament cluster: %v", cm.Name, err2))
			return
		}

		curConfigmap, err2 := cmInterface.Get(ctx, cm.Name, metav1.GetOptions{})
		if err2 == nil {
			// update it
			curConfigmap.Data = cm.Data
			_, err2 = cmInterface.Update(ctx, curConfigmap, metav1.UpdateOptions{})
			if err2 == nil {
				klog.InfoDepth(5, fmt.Sprintf("successfully create secret %s/%s to cluster-api manament cluster", curConfigmap.Namespace, cm.Name))
				cancel()
				return
			}
			klog.ErrorDepth(2, fmt.Sprintf("failed to update configmap %s/%s to cluster-api management cluster: %v", curConfigmap.Namespace, cm.Name, err2))
		}
	}, known.DefaultRetryPeriod, 0.3, true)

	return nil
}
