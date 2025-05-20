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

package deployer

import (
	"context"
	"fmt"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

func findAllMatchingSubscriptions(subLister applisters.SubscriptionLister, feedLabels map[string]string) ([]*appsapi.Subscription, []string, error) {
	// search all Subscriptions UID that referring this manifest
	subUIDs := sets.String{}
	for key, val := range feedLabels {
		// normally the length of a uuid is 36
		if len(key) != 36 || strings.Contains(key, "/") {
			continue
		}
		if val == subscriptionKind.Kind {
			subUIDs.Insert(key)
		}
	}

	var allRelatedSubscriptions []*appsapi.Subscription
	var allSubInfos []string
	// we just list all Subscriptions and filter them with matching UID,
	// since using label selector one by one does not improve too much performance
	subscriptions, err := subLister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	for _, sub := range subscriptions {
		// in case some subscriptions do not exist anymore, while labels still persist
		if subUIDs.Has(string(sub.UID)) && sub.DeletionTimestamp == nil {
			// perform strictly check
			// whether this Manifest is still referred as a feed in Subscription
			for _, feed := range sub.Spec.Feeds {
				if feed.Kind != feedLabels[known.ConfigKindLabel] {
					continue
				}
				if feed.Namespace != feedLabels[known.ConfigNamespaceLabel] {
					continue
				}
				if feed.Name != feedLabels[known.ConfigNameLabel] {
					continue
				}
				allRelatedSubscriptions = append(allRelatedSubscriptions, sub)
				allSubInfos = append(allSubInfos, klog.KObj(sub).String())
				break
			}
		}
	}

	return allRelatedSubscriptions, allSubInfos, err
}

func removeFeedFromAllMatchingSubscriptions(clusternetClient *clusternetclientset.Clientset,
	allRelatedSubscriptions []*appsapi.Subscription, feedLabels map[string]string) error {
	wg := sync.WaitGroup{}
	wg.Add(len(allRelatedSubscriptions))
	errCh := make(chan error, len(allRelatedSubscriptions))
	for _, sub := range allRelatedSubscriptions {
		go func(sub *appsapi.Subscription) {
			defer wg.Done()

			if err := utils.RemoveFeedFromSubscription(context.TODO(), clusternetClient, feedLabels, sub); err != nil {
				errCh <- err
			}
		}(sub)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	return utilerrors.NewAggregate(allErrs)
}

func GenerateLocalizationTemplate(base *appsapi.Base, overridePolicy appsapi.OverridePolicy) *appsapi.Localization {
	loc := &appsapi.Localization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", base.Name, utilrand.String(known.DefaultRandomIDLength)),
			Namespace: base.Namespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
			},
		},
		Spec: appsapi.LocalizationSpec{
			OverridePolicy: overridePolicy,
			Priority:       1000, // with the highest priority
		},
	}
	loc.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(base, baseKind)})
	return loc
}
