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

package defaultbinder

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
	"github.com/clusternet/clusternet/pkg/utils"
)

// DefaultBinder binds subscriptions to clusters using a clusternet client.
type DefaultBinder struct {
	handle framework.Handle
}

var _ framework.BindPlugin = &DefaultBinder{}

// New creates a DefaultBinder.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &DefaultBinder{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *DefaultBinder) Name() string {
	return names.DefaultBinder
}

// Bind binds subscriptions to clusters using the clusternet client.
func (pl *DefaultBinder) Bind(ctx context.Context, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	klog.V(3).InfoS("Attempting to bind subscription to clusters",
		"subscription", klog.KObj(sub), "clusters", targetClusters.BindingClusters)

	// use an ordered list
	sort.Stable(targetClusters)

	Status := sub.Status.DeepCopy()
	Status.BindingClusters = targetClusters.BindingClusters
	Status.Replicas = targetClusters.Replicas
	Status.SpecHash = utils.HashSubscriptionSpec(&sub.Spec)
	Status.DesiredReleases = len(targetClusters.BindingClusters)

	err := pl.UpdateSubscriptionStatus(sub, Status)
	if err != nil {
		return framework.AsStatus(err)
	}
	return nil
}

func (pl *DefaultBinder) UpdateSubscriptionStatus(sub *appsapi.Subscription, status *appsapi.SubscriptionStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update Subscription %q status", sub.Name)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sub.Status = *status
		_, err := pl.handle.ClientSet().AppsV1alpha1().Subscriptions(sub.Namespace).UpdateStatus(context.TODO(), sub, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return nil
		}

		if updated, err := pl.handle.ClientSet().AppsV1alpha1().Subscriptions(sub.Namespace).Get(context.TODO(), sub.Name, metav1.GetOptions{}); err == nil {
			// make a copy so we don't mutate the shared cache
			sub = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated Subscription %q from lister: %v", sub.Name, err))
		}
		return err
	})
}
