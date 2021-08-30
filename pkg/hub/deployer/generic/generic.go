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

package generic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	defaultRetries = 3
)

type Deployer struct {
	ctx context.Context

	clusterLister clusterlisters.ManagedClusterLister
	clusterSynced cache.InformerSynced
	secretLister  corev1lister.SecretLister
	secretSynced  cache.InformerSynced

	clusternetClient *clusternetclientset.Clientset

	descController *description.Controller

	recorder record.EventRecorder
}

func NewDeployer(ctx context.Context, clusternetClient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory, kubeInformerFactory kubeinformers.SharedInformerFactory,
	recorder record.EventRecorder) (*Deployer, error) {

	deployer := &Deployer{
		ctx:              ctx,
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		clusterSynced:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().HasSynced,
		secretLister:     kubeInformerFactory.Core().V1().Secrets().Lister(),
		secretSynced:     kubeInformerFactory.Core().V1().Secrets().Informer().HasSynced,
		clusternetClient: clusternetClient,
		recorder:         recorder,
	}

	descController, err := description.NewController(ctx,
		clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.recorder,
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descController = descController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Info("starting generic deployer...")
	defer klog.Info("shutting generic deployer")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(deployer.ctx.Done(),
		deployer.clusterSynced,
		deployer.secretSynced) {
		return
	}

	go deployer.descController.Run(workers, deployer.ctx.Done())

	<-deployer.ctx.Done()
}

func (deployer *Deployer) handleDescription(desc *appsapi.Description) error {
	klog.V(5).Infof("handle Description %s", klog.KObj(desc))
	if desc.Spec.Deployer != appsapi.DescriptionGenericDeployer {
		return nil
	}

	// check whether ManagedCluster will enable deploying Description with Pusher/Dual mode
	labelSet := labels.Set{}
	if len(desc.Labels[known.ClusterIDLabel]) > 0 {
		labelSet[known.ClusterIDLabel] = desc.Labels[known.ClusterIDLabel]
	}
	mcls, err := deployer.clusterLister.ManagedClusters(desc.Namespace).List(
		labels.SelectorFromSet(labelSet))
	if err != nil {
		return err
	}
	if mcls == nil {
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "ManagedClusterNotFound",
			fmt.Sprintf("can not find a ManagedCluster with uid=%s in current namespace", desc.Labels[known.ClusterIDLabel]))
		return fmt.Errorf("failed to find a ManagedCluster declaration in namespace %s", desc.Namespace)
	}
	if mcls[0].Spec.SyncMode == clusterapi.Pull || !mcls[0].Status.AppPusher {
		msg := "set SyncMode as Pull"
		if !mcls[0].Status.AppPusher {
			msg = "disabled AppPusher"
		}
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "", fmt.Sprintf("target cluster has %s", msg))
		klog.V(5).Infof("ManagedCluster with uid=%s has %s", mcls[0].UID, msg)
		return nil
	}

	if desc.DeletionTimestamp != nil {
		return deployer.deleteDescription(desc)
	}

	return deployer.createOrUpdateDescription(desc)
}

func (deployer *Deployer) createOrUpdateDescription(desc *appsapi.Description) error {
	dynamicClient, discoveryRESTMapper, err := deployer.getDynamicClient(desc)
	if err != nil {
		return err
	}

	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeployed := desc.Spec.Raw
	errCh := make(chan error, len(objectsToBeDeployed))
	for _, object := range objectsToBeDeployed {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
		} else {
			wg.Add(1)
			go func(resource *unstructured.Unstructured) {
				defer wg.Done()

				err := deployer.applyResourceWithRetry(dynamicClient, discoveryRESTMapper, resource, defaultRetries)
				if err != nil {
					errCh <- err
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	var statusPhase appsapi.DescriptionPhase
	var reason string
	err = utilerrors.NewAggregate(allErrs)
	if err != nil {
		statusPhase = appsapi.DescriptionPhaseFailure
		reason = err.Error()

		msg := fmt.Sprintf("failed to deploying Description %s: %v", klog.KObj(desc), err)
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "UnSuccessfullyDeployed", msg)
	} else {
		statusPhase = appsapi.DescriptionPhaseSuccess
		reason = ""

		msg := fmt.Sprintf("Description %s is deployed successfully", klog.KObj(desc))
		klog.V(5).Info(msg)
		deployer.recorder.Event(desc, corev1.EventTypeNormal, "SuccessfullyDeployed", msg)
	}

	// update status
	desc.Status.Phase = statusPhase
	desc.Status.Reason = reason
	_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(context.TODO(), desc, metav1.UpdateOptions{})

	return err
}

func (deployer *Deployer) deleteDescription(desc *appsapi.Description) error {
	dynamicClient, discoveryRESTMapper, err := deployer.getDynamicClient(desc)
	if err != nil {
		return err
	}

	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeleted := desc.Spec.Raw
	errCh := make(chan error, len(objectsToBeDeleted))
	for _, object := range objectsToBeDeleted {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
		} else {
			wg.Add(1)
			go func(resource *unstructured.Unstructured) {
				defer wg.Done()
				klog.V(5).Infof("deleting %s %s defined in Description %s", resource.GetKind(),
					klog.KObj(resource), klog.KObj(desc))
				err := deployer.deleteResourceWithRetry(dynamicClient, discoveryRESTMapper, resource, defaultRetries)
				if err != nil {
					errCh <- err
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	err = utilerrors.NewAggregate(allErrs)
	if err != nil {
		msg := fmt.Sprintf("failed to deleting Description %s: %v", klog.KObj(desc), err)
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "FailedDeletingDescription", msg)
	} else {
		klog.V(5).Infof("Description %s is deleted successfully", klog.KObj(desc))
	}
	return err
}

func (deployer *Deployer) applyResourceWithRetry(dynamicClient dynamic.Interface, restMapper meta.RESTMapper,
	resource *unstructured.Unstructured, retries int) error {
	// set UID as empty
	resource.SetUID("")

	backoff := retry.DefaultBackoff
	backoff.Steps = retries
	return wait.ExponentialBackoffWithContext(deployer.ctx, backoff, func() (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get RESTMapping: %v", err))
			return false, nil
		}

		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Create(context.TODO(), resource, metav1.CreateOptions{})
		if err == nil {
			return true, nil
		}
		if !apierrors.IsAlreadyExists(err) {
			klog.ErrorDepth(5, fmt.Sprintf("failed to create %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}

		// try  to update resource
		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Update(context.TODO(), resource, metav1.UpdateOptions{})
		if err == nil {
			return true, nil
		}
		statusCauses, ok := getStatusCause(err)
		if !ok {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get StatusCause for %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}

		curObj, err := dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}
		resourceCopy := resource.DeepCopy()
		for _, cause := range statusCauses {
			if cause.Type != metav1.CauseTypeFieldValueInvalid {
				continue
			}
			// apply immutable value
			fields := strings.Split(cause.Field, ".")
			setNestedField(resourceCopy, getNestedString(curObj.Object, fields...), fields...)
		}
		// update with immutable values applied
		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resourceCopy.GetNamespace()).
			Update(context.TODO(), resourceCopy, metav1.UpdateOptions{})
		if err == nil {
			return true, nil
		}
		klog.ErrorDepth(5, fmt.Sprintf("failed to update %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
		return false, nil
	})
}

func (deployer *Deployer) deleteResourceWithRetry(dynamicClient dynamic.Interface, restMapper meta.RESTMapper,
	resource *unstructured.Unstructured, retries int) error {
	backoff := retry.DefaultBackoff
	backoff.Steps = retries
	deletePropagationBackground := metav1.DeletePropagationBackground
	return wait.ExponentialBackoffWithContext(deployer.ctx, backoff, func() (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			msg := fmt.Sprintf("%v. Please check whether the advertised apiserver of current child cluster is accessible.", err)
			klog.WarningDepth(5, msg)
			return false, errors.New(msg)
		}

		err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Delete(context.TODO(), resource.GetName(), metav1.DeleteOptions{PropagationPolicy: &deletePropagationBackground})
		if err == nil || (err != nil && apierrors.IsNotFound(err)) {
			return true, nil
		}
		klog.ErrorDepth(5, "failed to delete %s %s: %v", resource.GetKind(), klog.KObj(resource), err)
		return false, nil
	})
}

func (deployer *Deployer) getDynamicClient(desc *appsapi.Description) (dynamic.Interface, meta.RESTMapper, error) {
	config, err := utils.GetChildClusterConfig(deployer.secretLister, deployer.clusterLister, desc.Namespace, desc.Labels[known.ClusterIDLabel])
	if err != nil {
		return nil, nil, err
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{})
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, err
	}
	restConfig.QPS = 5
	restConfig.Burst = 10

	kubeclient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}
	discoveryClient := cacheddiscovery.NewMemCacheClient(kubeclient.Discovery())
	discoveryRESTMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	return dynamicClient, discoveryRESTMapper, nil

}
