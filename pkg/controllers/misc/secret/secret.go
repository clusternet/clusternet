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

package secret

import (
	"context"
	"fmt"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreInformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	coreListers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = corev1.SchemeGroupVersion.WithKind("Secret")

type SyncHandlerFunc func(secret *corev1.Secret) error

// Controller is a controller that handles Secret
// here we only focus on Secret "child-cluster-deployer" !!!
type Controller struct {
	yachtController *yacht.Controller

	kubeClient   kubernetes.Interface
	secretLister coreListers.SecretLister
	recorder     record.EventRecorder
	syncHandler  SyncHandlerFunc
}

func NewController(
	kubeClient kubernetes.Interface,
	secretInformer coreInformers.SecretInformer,
	recorder record.EventRecorder,
	syncHandler SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		kubeClient:   kubeClient,
		secretLister: secretInformer.Lister(),
		recorder:     recorder,
		syncHandler:  syncHandler,
	}
	// create a yacht controller for secret
	yachtController := yacht.NewController("secret").
		WithCacheSynced(
			secretInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// here we only focus on Secret "child-cluster-deployer" !!!
			return isChildClusterDeployerSecret(oldObj) || isChildClusterDeployerSecret(newObj), nil
		})

	// Manage the addition/update of Secret
	_, err := secretInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	c.yachtController = yachtController
	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, ctx context.Context) {
	c.yachtController.WithWorkers(workers).Run(ctx)
}

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Secret resource
// with the current status of the resource.
func (c *Controller) handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	key := obj.(string)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing Secret %q", key)
	// Get the Secret resource with this name
	cachedSecret, err := c.secretLister.Secrets(ns).Get(name)
	// The Secret resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Secret %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// add finalizer
	secret := cachedSecret.DeepCopy()
	if !utils.ContainsString(secret.Finalizers, known.AppFinalizer) && secret.DeletionTimestamp == nil {
		secret.Finalizers = append(secret.Finalizers, known.AppFinalizer)
		if secret, err = c.kubeClient.CoreV1().Secrets(secret.Namespace).Update(context.TODO(),
			secret, metav1.UpdateOptions{}); err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Secret %s: %v", known.AppFinalizer, klog.KObj(secret), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(secret, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Secret %s", known.AppFinalizer, klog.KObj(secret))
		klog.V(4).Info(msg)
		c.recorder.Event(secret, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	secret.Kind = controllerKind.Kind
	secret.APIVersion = controllerKind.Version
	err = c.syncHandler(secret)
	if err != nil {
		c.recorder.Event(secret, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(secret, corev1.EventTypeNormal, "Synced", "Secret synced successfully")
		klog.Infof("successfully synced Secret %q", key)
	}
	return nil, err
}

func isChildClusterDeployerSecret(obj interface{}) bool {
	if obj == nil {
		return false
	}

	secret := obj.(*corev1.Secret)
	return secret.Name == known.ChildClusterSecretName
}
