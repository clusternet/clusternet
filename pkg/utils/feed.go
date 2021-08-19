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

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

const (
	// feed patch path in a Subscription
	feedPatchPath = "/spec/feeds"
)

var (
	helmChartKind = appsapi.SchemeGroupVersion.WithKind("HelmChart")
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
	}
	if len(feed.Namespace) > 0 {
		labelSet[known.ConfigNamespaceLabel] = feed.Namespace
	}
	if len(feed.Name) > 0 {
		labelSet[known.ConfigNameLabel] = feed.Name
	}
	selector := labels.SelectorFromSet(labelSet)

	if feed.FeedSelector != nil {
		feedSelector, err := metav1.LabelSelectorAsSelector(feed.FeedSelector)
		if err != nil {
			return nil, err
		}

		reqs, _ := feedSelector.Requirements()
		for _, r := range reqs {
			selector = selector.Add(r)
		}
	}

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

func ListChartsBySelector(chartLister applisters.HelmChartLister, feed appsapi.Feed) ([]*appsapi.HelmChart, error) {
	if chartLister == nil {
		return nil, errors.New("chartLister is nil when listing charts by selector")
	}

	selector, err := GetLabelsSelectorFromFeed(feed)
	if err != nil {
		return nil, err
	}

	return chartLister.HelmCharts(feed.Namespace).List(selector)
}

func FormatFeed(feed appsapi.Feed) string {
	if len(feed.Name) != 0 {
		return fmt.Sprintf("%s %s/%s", feed.Kind, feed.Namespace, feed.Name)
	}

	return fmt.Sprintf("%s with selector %q", feed.Kind, feed.FeedSelector.String())
}

func RemoveFeedFromSubscription(ctx context.Context, clusternetClient *clusternetclientset.Clientset,
	feedSourceLabels map[string]string, sub *appsapi.Subscription,
	chartLister applisters.HelmChartLister, manifestLister applisters.ManifestLister) error {
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

		if feed.FeedSelector != nil {
			// check whether this selector matches multiple objects
			if feed.Kind == helmChartKind.Kind {
				charts, err := chartLister.HelmCharts(feed.Namespace).List(feedSelector)
				if err != nil {
					return err
				}
				if len(charts) > 1 {
					continue
				}
			} else {
				manifests, err := manifestLister.Manifests(feed.Namespace).List(feedSelector)
				if err != nil {
					return err
				}
				if len(manifests) > 1 {
					continue
				}
			}
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

func IsManifestUsedBySubscription(manifestLister applisters.ManifestLister, manifest *appsapi.Manifest,
	sub *appsapi.Subscription) (bool, error) {
	var found bool
	for _, feed := range sub.Spec.Feeds {
		if feed.Kind == helmChartKind.Kind {
			continue
		}
		// list all referred Manifests
		manifests, err := ListManifestsBySelector(manifestLister, feed)
		if err != nil {
			return false, err
		}
		for _, mnst := range manifests {
			if mnst.UID == manifest.UID {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	return found, nil
}

func IsHelmChartUsedBySubscription(chartLister applisters.HelmChartLister, helmChart *appsapi.HelmChart,
	sub *appsapi.Subscription) (bool, error) {
	var found bool
	for _, feed := range sub.Spec.Feeds {
		if feed.Kind != helmChartKind.Kind {
			continue
		}
		// list all referred HelmCharts
		helmCharts, err := ListChartsBySelector(chartLister, feed)
		if err != nil {
			return false, err
		}
		for _, chart := range helmCharts {
			if chart.UID == helmChart.UID {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	return found, nil
}
