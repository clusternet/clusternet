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
	"context"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

const (
	// feed patch path in a Subscription
	feedPatchPath = "/spec/feeds"
)

func GetLabelsSelectorFromFeed(feed appsapi.Feed) (labels.Selector, error) {
	var gv schema.GroupVersion
	var err error
	if len(feed.APIVersion) > 0 {
		gv, err = schema.ParseGroupVersion(feed.APIVersion)
		if err != nil {
			return nil, err
		}
	}

	labelSet := labels.Set{
		known.ConfigGroupLabel:   gv.Group,
		known.ConfigVersionLabel: gv.Version,
		known.ConfigKindLabel:    feed.Kind,
		known.ConfigNameLabel:    feed.Name,
	}
	if len(feed.Namespace) > 0 {
		labelSet[known.ConfigNamespaceLabel] = feed.Namespace
	}
	selector := labels.SelectorFromSet(labelSet)

	return selector, nil
}

func ListManifestsBySelector(manifestLister applisters.ManifestLister, feed appsapi.Feed) ([]*appsapi.Manifest, error) {
	if manifestLister == nil {
		return nil, errors.New("manifestLister is nil when listing charts by selector")
	}

	selector, err := GetLabelsSelectorFromFeed(feed)
	if err != nil {
		return nil, err
	}
	return manifestLister.Manifests(appsapi.ReservedNamespace).List(selector)
}

func FormatFeed(feed appsapi.Feed) string {
	namespacedName := feed.Name
	if len(feed.Namespace) > 0 {
		namespacedName = fmt.Sprintf("%s/%s", feed.Namespace, feed.Name)
	}

	return fmt.Sprintf("%s %s", feed.Kind, namespacedName)
}

func RemoveFeedFromSubscription(ctx context.Context, clusternetClient *clusternetclientset.Clientset,
	feedSourceLabels map[string]string, sub *appsapi.Subscription) error {
	// we use JSONPatch for simplicity and efficiency
	var jsonPatchOptions []JsonPatchOption
	for idx, feed := range sub.Spec.Feeds {
		feedSelector, err := GetLabelsSelectorFromFeed(feed)
		if err != nil {
			return err
		}
		if !feedSelector.Matches(labels.Set(feedSourceLabels)) {
			continue
		}

		jsonPatchOptions = append(jsonPatchOptions, JsonPatchOption{
			Op:   "remove",
			Path: fmt.Sprintf("%s/%d", feedPatchPath, idx),
		})
	}

	if len(jsonPatchOptions) == 0 {
		return nil
	}
	patchBytes, err := json.Marshal(jsonPatchOptions)
	if err != nil {
		return err
	}
	_, err = clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Patch(ctx,
		sub.Name,
		types.JSONPatchType,
		patchBytes,
		metav1.PatchOptions{},
		"")
	return err
}

func FindObsoletedFeeds(oldFeeds []appsapi.Feed, newFeeds []appsapi.Feed) []appsapi.Feed {
	desiredFeedsMap := make(map[string]bool)
	for _, feed := range newFeeds {
		desiredFeedsMap[FormatFeed(feed)] = true
	}

	obsoleteFeeds := []appsapi.Feed{}
	for _, feed := range oldFeeds {
		if !desiredFeedsMap[FormatFeed(feed)] {
			obsoleteFeeds = append(obsoleteFeeds, feed)
		}
	}
	return obsoleteFeeds
}

func FindBasesFromUIDs(baseLister applisters.BaseLister, uids []string) []*appsapi.Base {
	allBases := []*appsapi.Base{}
	for _, uid := range uids {
		bases, err := baseLister.List(labels.SelectorFromSet(labels.Set{
			uid: "Base",
		}))
		if err != nil {
			klog.ErrorDepth(5, err)
			continue
		}
		allBases = append(allBases, bases...)
	}

	return allBases
}

func HasFeed(feed appsapi.Feed, feeds []appsapi.Feed) bool {
	for _, f := range feeds {
		if f.Kind == feed.Kind && f.Namespace == feed.Namespace && f.Name == feed.Name {
			return true
		}
	}

	return false
}
